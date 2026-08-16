# Changelog

Notable changes to raider-mate-service. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) without a `v` prefix.

The release workflow reads the section matching the pushed tag and uses it as the
GitHub Release body. A tag with no section here fails the release before anything is
published.

Sections are `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, `Security`.

## [Unreleased]

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
