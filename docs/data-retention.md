# Data retention: erasure after a guild leaves

**Status: promised in public, not built.** This is the next piece of work in this repo.

On 2026-08-20 the dashboard published a privacy policy and terms of service for the
hosted instance. The privacy policy states, in plain words a regulator would read
literally:

> When the bot is removed from a server, that server's events, signups, compositions and
> registered characters are **deleted within 30 days**. Re-inviting the bot inside those
> 30 days loses nothing.

Nothing in this repo implements that. The service is never told a guild removed the bot,
so the 30 day clock never starts and the purge never runs. Until this ships, the hosted
instance is making a commitment it does not keep, which is a worse problem than a missing
feature.

The policy's other erasure promise is already kept: `/character remove` in the bot
deletes a character and everything attached to it on the spot. The gap there is narrower,
and is item 4 below.

## What is missing

1. **The service never learns a guild is gone.** Discord sends `GUILD_DELETE` on the
   gateway, which only `raider-mate-bot` sees. Hard rule 6 stands: no `discordgo` type
   crosses into this repo, so the bot translates that event into a plain HTTP call.
2. **There is no guild row to mark.** Guild identity is a bare `discord_guild_id`
   scattered across `users`, `events`, `guild_settings`, `guild_channels`, `guild_roles`
   and `guild_raid_lead_roles`. Nothing records that a guild exists, so nothing can
   record that it left.
3. **Nothing schedules the purge.** `scheduled_jobs` is keyed on `event_id`, so it cannot
   currently carry a guild-scoped job.
4. **Per-raider erasure is half done.** `/character remove` in the bot already calls
   `DELETE /api/characters/{cid}`, and the cascades take that character's signups, comp
   slots, role choices and gear snapshots with it. What survives is the `users` row: a
   raider who removes their last character still leaves a `(discord_id,
   discord_guild_id)` pair behind, which is a Discord identity tied to a guild and so
   still personal data. The dashboard has no equivalent control yet.

## Shape of the work

### 1. A guild lifecycle row

Add `bot_removed_at timestamptz` to `guild_settings`, or a `guilds` table if a guild
needs identity beyond its settings. `guild_settings` is the cheaper option and already
has `discord_guild_id` as its primary key.

`NULL` means present. A timestamp means the clock is running. Re-invite clears it back to
`NULL`, which is what makes "re-inviting inside those 30 days loses nothing" true rather
than aspirational.

### 2. Two endpoints the bot calls

- `POST /api/guilds/{gid}/left` on `GUILD_DELETE`: stamps `bot_removed_at` and schedules
  the purge.
- `POST /api/guilds/{gid}/rejoined` on `GUILD_CREATE` for a guild already known: clears
  the stamp and cancels the pending purge.

Both are raid-lead-irrelevant: the caller is the bot with the shared key, not a person.
Decide deliberately whether an actor header is required at all.

### 3. A guild-scoped scheduled job

`scheduled_jobs.event_id` is `NOT NULL` and references `events`. A guild purge has no
event, so this needs either a nullable `discord_guild_id` column alongside it, or a
separate small table. Prefer whichever keeps the existing ticker loop unchanged.

### 4. The purge itself, in one transaction

The cascades already do most of the work. Verified against `00001_baseline.sql`:

- `DELETE FROM events WHERE discord_guild_id = $1` cascades to `comps`, `comp_slots`,
  `signups`, `late_signup_requests`, `notifications` and `scheduled_jobs`.
- `DELETE FROM users WHERE discord_guild_id = $1` cascades to `characters`, and from
  there to `character_roles`, `character_snapshots` and any remaining `signups`.

Four tables have no foreign key and need deleting by hand:

- `guild_settings`
- `guild_channels`
- `guild_roles`
- `guild_raid_lead_roles`

A raider in more than one guild keeps their other guilds: `users` is unique on
`(discord_id, discord_guild_id)`, so the row being deleted is the membership, not the
person.

### 5. Finish per-raider erasure

Characters are already covered by `DELETE /api/characters/{cid}`. Two things are left.

Delete the `users` row once its last character is gone, or expose an endpoint that
deletes it outright. Until then a raider who has removed everything is still recorded as
having been in that guild. Deleting the row cascades to anything of theirs that remains.

Decide whether a raid lead may erase somebody else's data or only their own, and write
the answer down here, because the privacy policy has to match it.

The dashboard also needs the control the bot already has. That is dashboard work, not
service work, but it is the same promise.

## How to know it works

An integration test that seeds a guild with events, signups, comps, a bench and
characters, then:

1. marks the guild left and runs the purge, asserting every one of the ten affected
   tables is empty for that guild;
2. asserts a second guild sharing a raider is untouched, including that raider's
   characters in the other guild;
3. marks the guild left, re-invites inside the window, and asserts nothing was deleted
   and no purge job remains pending.

Test three is the one that matters. It is the sentence a user will hold us to.

## While this is unbuilt

Character-level erasure is self-service already, which covers the request people actually
make. What is not covered is a guild leaving, and the leftover `users` row. Those arrive
at `dpo@phage.sk` and have to be served by hand inside the GDPR's one month. Do not let
that quietly become the permanent process.
