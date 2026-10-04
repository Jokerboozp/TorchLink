-- Database guard against overlapping duty: one row per person of every
-- assignment, kept by a trigger on fire_safety_record, with an exclusion
-- constraint on the person's time range. The fire safety service already
-- rejects overlaps; this catches any writer that bypasses it. The constraint
-- is checked at commit, because one save may remove an assignment and add
-- an overlapping one.
CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE TABLE IF NOT EXISTS duty_personnel_slot (
  tenant_id text NOT NULL, assignment_id text NOT NULL, personnel_id text NOT NULL,
  period int8range NOT NULL,
  PRIMARY KEY (tenant_id, assignment_id, personnel_id),
  CONSTRAINT duty_personnel_no_overlap EXCLUDE USING gist (tenant_id WITH =, personnel_id WITH =, period WITH &&) DEFERRABLE INITIALLY DEFERRED
);

CREATE OR REPLACE FUNCTION duty_personnel_slot_sync() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP <> 'INSERT' AND OLD.kind = 'assignment' THEN
    DELETE FROM duty_personnel_slot WHERE tenant_id = OLD.tenant_id AND assignment_id = OLD.id;
  END IF;
  IF TG_OP <> 'DELETE' AND NEW.kind = 'assignment' THEN
    INSERT INTO duty_personnel_slot(tenant_id, assignment_id, personnel_id, period)
    SELECT NEW.tenant_id, NEW.id, p, int8range((NEW.body->>'startAt')::bigint, (NEW.body->>'endAt')::bigint, '[)')
    FROM jsonb_array_elements_text(COALESCE(NEW.body->'personnelIds', '[]'::jsonb)) AS p;
  END IF;
  RETURN NULL;
END $$;

LOCK TABLE fire_safety_record IN SHARE ROW EXCLUSIVE MODE;
DROP TRIGGER IF EXISTS fire_safety_record_duty_slots ON fire_safety_record;
CREATE TRIGGER fire_safety_record_duty_slots AFTER INSERT OR UPDATE OR DELETE ON fire_safety_record
  FOR EACH ROW EXECUTE FUNCTION duty_personnel_slot_sync();

-- Backfill person by person; overlaps already stored are skipped (and must
-- be resolved when those assignments are next edited).
SET CONSTRAINTS duty_personnel_no_overlap IMMEDIATE;
DO $$
DECLARE
  slot record;
  skipped integer := 0;
BEGIN
  DELETE FROM duty_personnel_slot;
  FOR slot IN
    SELECT r.tenant_id, r.id, p AS personnel_id, int8range((r.body->>'startAt')::bigint, (r.body->>'endAt')::bigint, '[)') AS period
    FROM fire_safety_record r, jsonb_array_elements_text(COALESCE(r.body->'personnelIds', '[]'::jsonb)) AS p
    WHERE r.kind = 'assignment' ORDER BY (r.body->>'startAt')::bigint, r.id
  LOOP
    BEGIN
      INSERT INTO duty_personnel_slot VALUES (slot.tenant_id, slot.id, slot.personnel_id, slot.period);
    EXCEPTION WHEN exclusion_violation THEN
      skipped := skipped + 1;
    END;
  END LOOP;
  IF skipped > 0 THEN
    RAISE WARNING 'duty_personnel_slot: % existing overlapping duty slots were not indexed; edit those assignments to resolve them', skipped;
  END IF;
END $$;
SET CONSTRAINTS duty_personnel_no_overlap DEFERRED;
