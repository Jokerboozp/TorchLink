-- Open (ACTIVE or ACKED) alarms per device, kept by a trigger on
-- alarm_record so every write path stays consistent. Message processing reads
-- it together with the device state instead of probing alarm_record for each
-- message. A separate table is used because alarms can exist before the
-- device has a state row.
CREATE TABLE IF NOT EXISTS device_open_alarm (
  tenant_id text NOT NULL, device_id text NOT NULL, open_count integer NOT NULL DEFAULT 0,
  PRIMARY KEY (tenant_id, device_id)
);

CREATE OR REPLACE FUNCTION device_open_alarm_track() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  was_open boolean := false;
  is_open boolean := false;
BEGIN
  IF TG_OP <> 'INSERT' THEN was_open := OLD.status IN ('ACTIVE','ACKED'); END IF;
  IF TG_OP <> 'DELETE' THEN is_open := NEW.status IN ('ACTIVE','ACKED'); END IF;
  IF TG_OP = 'UPDATE' AND (OLD.tenant_id, OLD.device_id) IS DISTINCT FROM (NEW.tenant_id, NEW.device_id) THEN
    IF was_open THEN
      UPDATE device_open_alarm SET open_count = greatest(open_count - 1, 0) WHERE tenant_id = OLD.tenant_id AND device_id = OLD.device_id;
    END IF;
    IF is_open THEN
      INSERT INTO device_open_alarm VALUES (NEW.tenant_id, NEW.device_id, 1)
      ON CONFLICT (tenant_id, device_id) DO UPDATE SET open_count = device_open_alarm.open_count + 1;
    END IF;
    RETURN NULL;
  END IF;
  IF was_open = is_open THEN RETURN NULL; END IF;
  IF is_open THEN
    INSERT INTO device_open_alarm VALUES (NEW.tenant_id, NEW.device_id, 1)
    ON CONFLICT (tenant_id, device_id) DO UPDATE SET open_count = device_open_alarm.open_count + 1;
  ELSE
    UPDATE device_open_alarm SET open_count = greatest(open_count - 1, 0) WHERE tenant_id = OLD.tenant_id AND device_id = OLD.device_id;
  END IF;
  RETURN NULL;
END $$;

-- Writers wait for the short backfill so no alarm change is counted twice
-- or missed.
LOCK TABLE alarm_record IN SHARE ROW EXCLUSIVE MODE;
DROP TRIGGER IF EXISTS alarm_record_open_count ON alarm_record;
CREATE TRIGGER alarm_record_open_count AFTER INSERT OR DELETE OR UPDATE OF status, tenant_id, device_id ON alarm_record
  FOR EACH ROW EXECUTE FUNCTION device_open_alarm_track();
DELETE FROM device_open_alarm;
INSERT INTO device_open_alarm(tenant_id, device_id, open_count)
SELECT tenant_id, device_id, count(*) FROM alarm_record WHERE status IN ('ACTIVE','ACKED') GROUP BY tenant_id, device_id;
