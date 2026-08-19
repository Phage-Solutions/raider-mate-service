# Changelog

Notable changes to raider-mate-service. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) without a `v` prefix.

The release workflow reads the section matching the pushed tag and uses it as the
GitHub Release body. A tag with no section here fails the release before anything is
published.

Sections are `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, `Security`.

## [Unreleased]

## [0.4.0] - 2026-08-19

### Changed

- Players may write `LATE` and `ABSENT` until `starts_at`, past `signup_deadline`. Both
  report what is happening on the night, so they are accepted outright rather than
  filed as a late request. Every other status, and a withdrawal, still closes at the
  deadline.

### Removed

- `COMP_NAG`. Nothing schedules the job and nothing sends the notification: locking a
  comp is optional, so an unlocked one is no longer chased two hours out. The enum
  values stay for now, and jobs an older release scheduled are drained without
  notifying anyone.

## [0.3.1] - 2026-08-16

### Added

- Events carry a `reminder_lead_minutes`: how long before the start the pre-event
  reminder fires. `POST` and `PATCH /api/events/{id}` accept it (0 to 1440, where 0
  means no reminder), and a create that omits it takes the guild's default, then 30
  minutes. The value is resolved once and stored on the event, so changing the guild
  default later does not re-time a raid that is already posted.
- Guild settings carry `reminder_lead_minutes` and `reminder_delivery` (`PING`, `DM` or
  `BOTH`, default `PING`), which decide the default lead time and whether the reminder
  arrives as one channel post mentioning everyone or as a DM each.
- `CHANNEL` notifications: a message posted in a channel that mentions the users in the
  new `discord_ids` field. `role_ids` is unchanged and still means role mentions.

### Changed

- The pre-event reminder now goes to every distinct user with a `CONFIRMED`, `LATE` or
  `TENTATIVE` signup, rather than only those holding an assigned comp slot. A raider
  left out of a locked roster used to hear nothing. Alts still collapse to one recipient.
- Migration `00005` renames `REMINDER_1H` to `REMINDER_PRE_EVENT` in `job_enum` and
  `notification_kind`, since the hour is now a setting. Existing scheduled jobs and
  notifications follow the rename with no backfill. Bots must understand the new kind
  before this release goes out; the current bot release accepts both.
- Migration `00006` adds the settings columns, `events.reminder_lead_minutes`,
  `notifications.discord_ids`, and extends the notification target check for `CHANNEL`.

### Fixed

- Characters registered with a realm as it reads in game ("Twisting Nether") or a
  region in capitals ("EU") never synced from Raider.IO: the fetch was rejected every
  time, and a rejected fetch leaves `last_synced` NULL on purpose, so those characters
  showed no ilvl or Mythic+ score indefinitely with nothing in the API to say why.
  `POST /api/guilds/{gid}/characters` now stores the canonical slug form of `realm` and
  a lowercase `region`, and migration `00004` rewrites the rows already on file. Clients
  may keep sending a realm as the raider typed it; the `realm` in a character response
  is now the slug, which is also what `raiderio_url` has always used. Duplicate
  registrations that differ only in realm spelling now collide as they should.
- A Raider.IO access key the API rejects no longer consumes an entire sync batch before
  the worker gives up on the tick. It aborts on the first rejection, as it already did
  for rate limiting, so the affected characters keep their queue position and sync on
  the next tick once the key is corrected.

## [0.3.0] - 2026-08-16

### Added

- `GET`/`PUT /api/guilds/{gid}/discord-channels` and `GET`/`PUT /api/guilds/{gid}/discord-roles`:
  a per-guild catalog of Discord channels and roles. The `PUT` is the bot pushing its
  own view of the guild (shared-key auth, no actor), replacing the whole catalog each
  time. The `GET` is guild-admin only and backs a dashboard picker for
  `guild_settings.events_channel_id` and `event_mention_role_ids`, neither of which had
  a source of "what's actually in this guild" to pick from before this.

## [0.2.0] - 2026-08-16

### Added

- `raiderio_url` on every character shape the API returns: the full character resource
  and the summary embedded in signup rows and comp slots. It is the character's
  Raider.IO page, so the bot can link a raider's name straight from an event embed
  without rebuilding the URL from fields the summary does not carry. It is a plain
  field rather than a `_links` entry, because a missing link means "unavailable to you
  right now" and this page is always available.
- `RAIDERIO_ACCESS_KEY` on the worker, sent with every Raider.IO request. Register an
  application at https://raider.io/settings/apps to raise the request rate above what
  anonymous access allows. Optional: with no key the worker syncs anonymously, exactly
  as it did before.

### Security

- The Raider.IO access key is kept out of the worker's logs. Raider.IO takes the key as
  a query parameter, and a failed request's error prints the URL it failed on, so a
  transport error would otherwise have written the key to the log stream on every
  network blip.

## [0.1.1] - 2026-08-16

### Fixed

- Registering a second character no longer fails. `is_main` on
  `POST /api/guilds/{gid}/characters` is now decided by the service and granted only
  while the raider has no main yet, instead of being written straight through to a
  column guarded by a one-main-per-raider unique index. A client that sends
  `is_main: true` on every registration, which is what raider-mate-bot does, is
  therefore safe: the first character becomes the main and later ones do not take the
  flag from it. Every registration after a raider's first previously returned 500.
- `PATCH /api/characters/{cid}` with `is_main: true` now demotes the current main
  before promoting the new one, so switching mains works. It previously returned 500
  whenever the raider already had a main, which is every case worth switching from.
  The dashboard's switch-mains flow depends on this.
- Re-registering a character the raider already has returns 409 with a message meant
  for a player, instead of 500 and "internal error". The bot shows a service message
  only below 500, so this reached raiders as "the roster service is having a bad time".

### Changed

- Registration writes the user and the character in one transaction. A failure between
  the two previously left a user row owning no characters, which nothing cleaned up.

## [0.1.0] - 2026-08-16

Initial release: signups with multi-role, one comp view, reminders, and the API
surface the bot and dashboard need for those.

### Added

- Guild-scoped REST API with HATEOAS links computed per response from the caller's
  permissions and the resource's current state. A missing link means the action is
  unavailable to this caller right now, not an oversight.
- Actor auth: `X-Actor-Discord-Id`, `-Guild-Id`, `-Roles`, `-Guild-Admin` headers plus
  a service API key, checked with a constant-time compare.
- Raid-lead capability: a guild maps its own Discord role IDs to raid-lead rather than
  the service hardcoding a role name. A guild with no mapped roles grants the
  capability to admins only.
- Character registration and role menus, kept in sync against the Raider.IO API.
  Snapshots are cached and refreshed by a background worker; never fetched from a
  request path.
- Events with multi-role signups. A signup means "I am coming, here is my role menu";
  assignment happens later, and a role lives on the character, not the signup.
- A deadline gate on signup writes: a raid-lead write always passes. `ABSENT` and
  `NO_SHOW` are raid-lead-only regardless of the deadline.
- Late requests: a player write past `signup_deadline` returns 202 with a request
  instead of a dead end. A raid lead approves or rejects it.
- Comp assignment algorithm, manual override, and a lock that freezes a comp's roster
  for the raid.
- Scheduled reminders (`REMINDER_24H`, `REMINDER_1H`, `SIGNUP_DEADLINE`, `COMP_NAG`),
  computed at event create/edit time and drained by a background worker polling
  `scheduled_jobs`.
- Notification outbox for the bot: claim-and-deliver over `GET`/`PATCH` on
  `/api/notifications`, plus a Server-Sent Events stream at
  `GET /api/notifications/stream` that wakes on a Postgres `LISTEN`/`NOTIFY` trigger
  instead of polling.
- Guild settings: IANA timezone, event mention role IDs, event banner URL.
- `allowed_statuses` on signup responses: what the calling actor may `PUT`, so a client
  renders the statuses it has rather than discovering a 403.
- `COMP_SLOT_DROPPED` notification, queued when a signup write empties a locked comp.

### Changed

- `ABSENT` is self-reported. A raider declares their own planned absence, which is
  wider than `DECLINED`'s "not this event". `NO_SHOW` stays raid-lead-only.
- A signup that leaves the assignment pool gives up its `comp_slots` rows in the same
  transaction, instead of holding a seat in a locked comp it will not fill. A
  withdrawal counts, and its `COMP_SLOT_DROPPED` carries no status, since the signup is
  gone rather than restated.
- A signup or late-request write and the notification reporting it share one
  transaction, so neither can land without the other. `LateRequests.Approve` reads and
  decides in that same transaction, which closes a race where two raid leads hitting
  the same button both got past the already-decided guard.
- The `SIGNUP_DEADLINE` notification payload carries a count for every status, zeros
  included, so a client can render "0 absent".
- Notification delivery is claim-then-deliver rather than ack-by-id, closing the gap
  where a crash between send and ack could drop a notification.
- Requires PostgreSQL 17. Migrations were squashed into a single baseline; a
  self-hosted instance on an older cluster needs to upgrade before running
  `goose up`.
