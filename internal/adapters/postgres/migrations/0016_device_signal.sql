-- Latest health signals per device (stuck values, report drift, out-of-range
-- values, outliers among devices of one template), replaced per tenant by
-- the device-signals job.
CREATE TABLE IF NOT EXISTS device_signal (
  tenant_id text NOT NULL,
  device_id text NOT NULL,
  signal_type text NOT NULL,
  property text NOT NULL DEFAULT '',
  product_id text NOT NULL DEFAULT '',
  strength double precision NOT NULL,
  window_start bigint NOT NULL,
  window_end bigint NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}',
  updated_at bigint NOT NULL,
  PRIMARY KEY (tenant_id, device_id, signal_type, property)
);
CREATE INDEX IF NOT EXISTS device_signal_strength_idx ON device_signal (tenant_id, strength DESC);
