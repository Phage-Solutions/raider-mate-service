-- +goose Up
-- Past signup_deadline, a player write becomes a request instead of an error the bot
-- has to invent a message for. status is what the raider asked for, so approving is a
-- plain UpsertSignup with it. Named for when a request happens, not for the status it
-- carries: a withdrawal past the deadline is a request carrying DECLINED.
CREATE TYPE request_state AS ENUM ('PENDING', 'APPROVED', 'REJECTED');

CREATE TABLE late_signup_requests (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    event_id      uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    character_id  uuid NOT NULL REFERENCES characters (id) ON DELETE CASCADE,
    status        signup_status NOT NULL,
    note          text,
    state         request_state NOT NULL DEFAULT 'PENDING',
    created_at    timestamptz NOT NULL DEFAULT now(),
    decided_at    timestamptz,
    -- A re-request upserts rather than piling up rows.
    UNIQUE (event_id, character_id)
);

-- +goose Down
DROP TABLE late_signup_requests;
DROP TYPE request_state;
