-- migrate:no-transaction
-- Prepare raw_archive_index, raw_message_log, standard_message,
-- device_state_event and audit_log for monthly range partitioning
-- (migration 11 switches them). Each table gets, while ingest keeps
-- writing: a unique index that includes its partition key, and a validated
-- CHECK that every row lies before the cutover, so the existing table can be
-- attached as one "legacy" partition without copying or rescanning it.
-- The cutover is the start of the month after next (UTC).
ALTER TABLE standard_message ADD COLUMN IF NOT EXISTS created_at bigint NOT NULL DEFAULT ((extract(epoch FROM now()) * 1000)::bigint)
;
CREATE TABLE IF NOT EXISTS standard_message_key (
  tenant_id text NOT NULL, message_id text NOT NULL, created_at bigint NOT NULL,
  PRIMARY KEY (tenant_id, message_id)
)
;
CREATE INDEX IF NOT EXISTS standard_message_key_created_idx ON standard_message_key(created_at)
;
CREATE TABLE IF NOT EXISTS partition_cutover (
  table_name text PRIMARY KEY, cutover_ms bigint NOT NULL, prepared_at_ms bigint NOT NULL
)
;
INSERT INTO partition_cutover(table_name, cutover_ms, prepared_at_ms)
SELECT t, (extract(epoch FROM date_trunc('month', now() AT TIME ZONE 'UTC') + interval '2 month') * 1000)::bigint, (extract(epoch FROM now()) * 1000)::bigint
FROM unnest(ARRAY['raw_archive_index','raw_message_log','standard_message','device_state_event','audit_log']) AS t
ON CONFLICT DO NOTHING
;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS raw_archive_index_partition_key ON raw_archive_index(tenant_id, message_id, received_at)
;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS raw_message_log_partition_key ON raw_message_log(tenant_id, message_id, received_at)
;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS standard_message_partition_key ON standard_message(tenant_id, message_id, created_at)
;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS device_state_event_partition_key ON device_state_event(id, created_at)
;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS audit_log_partition_key ON audit_log(id, created_at)
;
DO $$
DECLARE
  spec record;
  bound text;
BEGIN
  FOR spec IN SELECT c.table_name, c.cutover_ms,
      CASE c.table_name WHEN 'raw_archive_index' THEN 'received_at' WHEN 'raw_message_log' THEN 'received_at'
        WHEN 'standard_message' THEN 'created_at' ELSE 'created_at' END AS key_column
    FROM partition_cutover c JOIN pg_class r ON r.oid = to_regclass(c.table_name) AND r.relkind = 'r'
    WHERE NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = r.oid AND conname = c.table_name || '_partition_bound')
  LOOP
    bound := spec.cutover_ms::text;
    IF spec.table_name = 'device_state_event' THEN
      bound := quote_literal(to_timestamp(spec.cutover_ms / 1000.0)) || '::timestamptz';
    END IF;
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (%I < %s) NOT VALID', spec.table_name, spec.table_name || '_partition_bound', spec.key_column, bound);
  END LOOP;
END $$
;
ALTER TABLE raw_archive_index VALIDATE CONSTRAINT raw_archive_index_partition_bound
;
ALTER TABLE raw_message_log VALIDATE CONSTRAINT raw_message_log_partition_bound
;
ALTER TABLE standard_message VALIDATE CONSTRAINT standard_message_partition_bound
;
ALTER TABLE device_state_event VALIDATE CONSTRAINT device_state_event_partition_bound
;
ALTER TABLE audit_log VALIDATE CONSTRAINT audit_log_partition_bound
;
