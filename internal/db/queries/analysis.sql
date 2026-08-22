-- Every query here is bounded by an explicit window rather than by now(): the cutoff
-- and the upper bound both arrive as parameters so a test can pin the clock, and so
-- one request's panels all describe the same stretch of time.

-- name: CountEventsInWindow :one
-- The denominator every attendance percentage divides by. Counted once rather than
-- per character, because a raider who signed up to nothing has no rows to count and
-- would otherwise vanish from their own attendance rate.
SELECT count(*)::bigint FROM events
WHERE discord_guild_id = sqlc.arg(guild_id)
  AND starts_at >= sqlc.arg(since)
  AND starts_at < sqlc.arg(until);

-- name: AttendanceByCharacter :many
-- One row per character that answered at least once in the window, with the statuses
-- split out rather than summed. NO_SHOW and DECLINED are the pair that matters: since
-- 0.7.0 a raid lead may write NO_SHOW and nothing else on someone else's signup, so
-- "said no in advance" and "did not turn up" are finally different facts.
SELECT
    c.id                                                     AS character_id,
    c.name,
    c.realm,
    c.class,
    count(*) FILTER (WHERE s.status = 'CONFIRMED')::bigint    AS confirmed,
    count(*) FILTER (WHERE s.status = 'TENTATIVE')::bigint    AS tentative,
    count(*) FILTER (WHERE s.status = 'DECLINED')::bigint     AS declined,
    count(*) FILTER (WHERE s.status = 'LATE')::bigint         AS late,
    count(*) FILTER (WHERE s.status = 'ABSENT')::bigint       AS absent,
    count(*) FILTER (WHERE s.status = 'NO_SHOW')::bigint      AS no_show
FROM signups s
JOIN events e ON e.id = s.event_id
JOIN characters c ON c.id = s.character_id
WHERE e.discord_guild_id = sqlc.arg(guild_id)
  AND e.starts_at >= sqlc.arg(since)
  AND e.starts_at < sqlc.arg(until)
GROUP BY c.id, c.name, c.realm, c.class
ORDER BY c.name, c.realm;

-- name: CompRoleTotals :many
-- What the guild actually fielded, by role, across every comp in the window. Roster
-- and bench counted apart: a role that is always on the bench is oversupplied, and a
-- combined total hides exactly that.
SELECT
    cs.role,
    count(*) FILTER (WHERE NOT cs.is_bench)::bigint AS placed,
    count(*) FILTER (WHERE cs.is_bench)::bigint     AS benched
FROM comp_slots cs
JOIN events e ON e.id = cs.event_id
WHERE e.discord_guild_id = sqlc.arg(guild_id)
  AND e.starts_at >= sqlc.arg(since)
  AND e.starts_at < sqlc.arg(until)
GROUP BY cs.role
ORDER BY cs.role;

-- name: BenchByCharacter :many
-- The bench fairness tracker. Ordered by how often somebody sat out, because the
-- question this answers is always "who has been carrying the bench".
SELECT
    c.id                                            AS character_id,
    c.name,
    c.realm,
    c.class,
    count(*)::bigint                                AS boards,
    count(*) FILTER (WHERE cs.is_bench)::bigint     AS benched
FROM comp_slots cs
JOIN events e ON e.id = cs.event_id
JOIN characters c ON c.id = cs.character_id
WHERE e.discord_guild_id = sqlc.arg(guild_id)
  AND e.starts_at >= sqlc.arg(since)
  AND e.starts_at < sqlc.arg(until)
GROUP BY c.id, c.name, c.realm, c.class
ORDER BY benched DESC, c.name, c.realm;

-- name: RoleCoverage :many
-- What the roster CAN play, which is the roles on the character and not the roles a
-- comp happened to use. Priority 1 counted separately: a guild with nine healers who
-- would all rather be dps is not a guild with nine healers.
SELECT
    cr.role,
    count(*)::bigint                                  AS characters,
    count(*) FILTER (WHERE cr.priority = 1)::bigint   AS first_choice
FROM character_roles cr
JOIN characters c ON c.id = cr.character_id
JOIN users u ON u.id = c.user_id
WHERE u.discord_guild_id = sqlc.arg(guild_id)
GROUP BY cr.role
ORDER BY cr.role;

-- name: RosterActivity :one
-- Roster size against how much of it is still turning up. Dormant is the difference,
-- computed by the caller rather than selected, so the two numbers cannot disagree.
SELECT
    count(*)::bigint                                              AS characters,
    count(*) FILTER (WHERE c.is_main)::bigint                     AS mains,
    count(*) FILTER (WHERE active.character_id IS NOT NULL)::bigint AS active
FROM characters c
JOIN users u ON u.id = c.user_id
LEFT JOIN (
    SELECT DISTINCT s.character_id
    FROM signups s
    JOIN events e ON e.id = s.event_id
    WHERE e.discord_guild_id = sqlc.arg(guild_id)
      AND e.starts_at >= sqlc.arg(since)
      AND e.starts_at < sqlc.arg(until)
) active ON active.character_id = c.id
WHERE u.discord_guild_id = sqlc.arg(guild_id);

-- name: EventThroughput :many
-- Weekly, and counted in people rather than in rows.
--
-- A bar is a week, and a week holds however many raids the guild ran. Summing each
-- raid's signups made twelve raiders across three nights read as thirty-six, which is
-- more people than the guild has and more than the roster it is drawn beside.
--
-- count(DISTINCT character_id) is also what lets signups and comp_slots hang off the
-- same row without multiplying each other: the join duplicates rows, and the distinct
-- collapses them back. That is why this reads as a plain join rather than the pair of
-- laterals it needed when it was summing.
SELECT
    date_trunc('week', e.starts_at)::timestamptz                                   AS week,
    count(DISTINCT e.id)::bigint                                                   AS events,
    count(DISTINCT s.character_id) FILTER (WHERE s.status = 'CONFIRMED')::bigint   AS confirmed,
    count(DISTINCT s.character_id) FILTER (WHERE s.status = 'DECLINED')::bigint    AS declined,
    count(DISTINCT s.character_id) FILTER (WHERE s.status = 'NO_SHOW')::bigint     AS no_show,
    count(DISTINCT cs.character_id) FILTER (WHERE NOT cs.is_bench)::bigint         AS placed,
    count(DISTINCT cs.character_id) FILTER (WHERE cs.is_bench)::bigint             AS benched
FROM events e
LEFT JOIN signups s ON s.event_id = e.id
LEFT JOIN comp_slots cs ON cs.event_id = e.id
WHERE e.discord_guild_id = sqlc.arg(guild_id)
  AND e.starts_at >= sqlc.arg(since)
  AND e.starts_at < sqlc.arg(until)
GROUP BY week
ORDER BY week;

-- name: IlvlSeries :many
-- Mains only, and quartiles rather than extremes.
--
-- Both are the same fix for the same thing. A guild's roster holds abandoned alts and
-- half-levelled bank characters, so min over every character is whichever level-20 was
-- registered and never touched again, and a band drawn from it spans two hundred item
-- levels of nothing. A raider's main is what the guild actually raids with, and the
-- middle half of those is a spread that one returning player cannot move.
--
-- A sync writes a row whenever Raider.IO moved, so a character who reforged four times
-- in a week would otherwise weigh four times as much as one who logged off: DISTINCT ON
-- takes that character's last snapshot in each week, and the aggregate then sees one row
-- per character per week.
--
-- double precision rather than numeric because these are summary statistics being drawn
-- as a chart, and pgtype.Numeric buys nothing at the far end of that.
WITH weekly AS (
    SELECT DISTINCT ON (sn.character_id, date_trunc('week', sn.captured_at))
        date_trunc('week', sn.captured_at)::timestamptz AS week,
        sn.character_id,
        sn.ilvl
    FROM character_snapshots sn
    JOIN characters c ON c.id = sn.character_id
    JOIN users u ON u.id = c.user_id
    WHERE u.discord_guild_id = sqlc.arg(guild_id)
      AND c.is_main
      AND sn.captured_at >= sqlc.arg(since)
      AND sn.captured_at < sqlc.arg(until)
      AND sn.ilvl IS NOT NULL
    ORDER BY sn.character_id, date_trunc('week', sn.captured_at), sn.captured_at DESC, sn.id DESC
)
SELECT
    week,
    count(*)::bigint AS characters,
    (percentile_cont(0.25) WITHIN GROUP (ORDER BY ilvl::double precision))::double precision AS p25_ilvl,
    (percentile_cont(0.5) WITHIN GROUP (ORDER BY ilvl::double precision))::double precision AS median_ilvl,
    (percentile_cont(0.75) WITHIN GROUP (ORDER BY ilvl::double precision))::double precision AS p75_ilvl
FROM weekly
GROUP BY week
ORDER BY week;
