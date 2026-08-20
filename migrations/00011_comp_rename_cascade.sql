-- +goose Up
-- comp_slots points at its comp by (event_id, comp_name), and the original foreign key
-- cascades a delete but says nothing about an update. That makes the name effectively
-- immutable: renaming a comp that holds any slots fails the constraint, whichever of
-- the two tables is written first, because neither order leaves a moment when the
-- rows agree.
--
-- Cascading the update is what makes a rename one statement instead of a delete and a
-- rebuild, which would throw the board away to change a label.
ALTER TABLE comp_slots
    DROP CONSTRAINT comp_slots_comp_fkey;

ALTER TABLE comp_slots
    ADD CONSTRAINT comp_slots_comp_fkey
        FOREIGN KEY (event_id, comp_name) REFERENCES comps (event_id, name)
        ON DELETE CASCADE
        ON UPDATE CASCADE;

-- +goose Down
ALTER TABLE comp_slots
    DROP CONSTRAINT comp_slots_comp_fkey;

ALTER TABLE comp_slots
    ADD CONSTRAINT comp_slots_comp_fkey
        FOREIGN KEY (event_id, comp_name) REFERENCES comps (event_id, name)
        ON DELETE CASCADE;
