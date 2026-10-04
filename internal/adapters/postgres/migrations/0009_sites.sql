-- Units, buildings, floors and points (sites), one row per record with the
-- tenant's revision checked on every save, like fire_safety_record.
CREATE TABLE IF NOT EXISTS site_revision (
  tenant_id text PRIMARY KEY, revision bigint NOT NULL
);
CREATE TABLE IF NOT EXISTS site_record (
  tenant_id text NOT NULL, kind text NOT NULL, id text NOT NULL,
  seq bigserial NOT NULL,
  body jsonb NOT NULL,
  device_id text GENERATED ALWAYS AS (body->>'deviceId') STORED,
  PRIMARY KEY (tenant_id, kind, id)
);
CREATE INDEX IF NOT EXISTS site_record_order_idx ON site_record(tenant_id, seq);
CREATE INDEX IF NOT EXISTS site_record_device_idx ON site_record(tenant_id, device_id) WHERE device_id IS NOT NULL;
