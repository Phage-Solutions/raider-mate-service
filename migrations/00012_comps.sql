-- +goose Up
-- Until now a comp existed only implicitly, as whatever comp_name its slots carried.
-- That cannot express an empty comp, and it cannot say who owns one: a raid lead
-- building a comp by hand needs the assigner to keep its hands off, and the assigner
-- needs somewhere to read that from.
CREATE TYPE comp_mode AS ENUM ('AUTO', 'MANUAL');

CREATE TABLE comps (
    id        uuid PRIMARY KEY DEFAULT uuidv7(),
    event_id  uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    name      text NOT NULL,
    mode      comp_mode NOT NULL DEFAULT 'AUTO',
    UNIQUE (event_id, name)
);

-- Existing slots predate the table, so give them their comps before the FK lands.
INSERT INTO comps (event_id, name)
SELECT DISTINCT event_id, comp_name FROM comp_slots;

ALTER TABLE comp_slots ADD CONSTRAINT comp_slots_comp_fkey
    FOREIGN KEY (event_id, comp_name) REFERENCES comps (event_id, name)
    ON DELETE CASCADE;

-- +goose Down
ALTER TABLE comp_slots DROP CONSTRAINT comp_slots_comp_fkey;
DROP TABLE comps;
DROP TYPE comp_mode;
