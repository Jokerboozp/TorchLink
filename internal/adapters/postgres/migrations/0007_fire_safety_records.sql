-- One row per fire safety record (station, personnel, equipment, dispatch,
-- duty shift / assignment / swap, extinguisher, inspection) and one
-- revision per tenant checked on every save.
CREATE TABLE IF NOT EXISTS fire_safety_revision (
  tenant_id text PRIMARY KEY, revision bigint NOT NULL
);
CREATE TABLE IF NOT EXISTS fire_safety_record (
  tenant_id text NOT NULL, kind text NOT NULL, id text NOT NULL,
  seq bigserial NOT NULL,
  body jsonb NOT NULL,
  station_id text GENERATED ALWAYS AS (body->>'stationId') STORED,
  status text GENERATED ALWAYS AS (body->>'status') STORED,
  PRIMARY KEY (tenant_id, kind, id)
);
CREATE INDEX IF NOT EXISTS fire_safety_record_order_idx ON fire_safety_record(tenant_id, seq);
CREATE INDEX IF NOT EXISTS fire_safety_record_station_idx ON fire_safety_record(tenant_id, kind, station_id);
CREATE INDEX IF NOT EXISTS fire_safety_record_status_idx ON fire_safety_record(tenant_id, kind, status);
