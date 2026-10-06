-- migrate:no-transaction
-- The device list pages newest first within a tenant.
CREATE INDEX CONCURRENTLY IF NOT EXISTS device_registry_updated_idx ON device_registry(tenant_id, updated_at DESC, id DESC)
;
