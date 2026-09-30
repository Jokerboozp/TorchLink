CREATE TABLE IF NOT EXISTS alarm_observation_input (
 tenant_id text NOT NULL, input_key text NOT NULL, content_hash text NOT NULL,
 PRIMARY KEY(tenant_id,input_key)
);
CREATE TABLE IF NOT EXISTS alarm_observation (
 tenant_id text NOT NULL,id text NOT NULL,slot_key text NOT NULL,device_id text NOT NULL,
 component_id text NOT NULL DEFAULT '',alarm_type text NOT NULL,origin_kind text NOT NULL,signal_key text NOT NULL,
 event_at bigint NOT NULL,recorded_at bigint NOT NULL,acceptance text NOT NULL,fact_kind text NOT NULL,
 alarm_id text NOT NULL DEFAULT '',content_hash text NOT NULL,body jsonb NOT NULL,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,slot_key)
);
CREATE INDEX IF NOT EXISTS alarm_observation_signal_time ON alarm_observation(tenant_id,device_id,component_id,alarm_type,origin_kind,signal_key,event_at,id);
CREATE INDEX IF NOT EXISTS alarm_observation_recorded ON alarm_observation(tenant_id,recorded_at,id);
CREATE TABLE IF NOT EXISTS alarm_observation_conflict (
 tenant_id text NOT NULL,id text NOT NULL,observation_id text NOT NULL DEFAULT '',recorded_at bigint NOT NULL,body jsonb NOT NULL,
 PRIMARY KEY(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS alarm_observation_attempt (
 seq bigserial PRIMARY KEY,tenant_id text NOT NULL,observation_id text NOT NULL,recorded_at bigint NOT NULL,body jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS alarm_observation_collection (
 tenant_id text NOT NULL,device_id text NOT NULL,source_system text NOT NULL,collection_started_at bigint NOT NULL,
 backfill_status text NOT NULL DEFAULT 'NOT_STARTED',historical_quality text NOT NULL DEFAULT 'PARTIAL',body jsonb NOT NULL DEFAULT '{}',
 PRIMARY KEY(tenant_id,device_id,source_system)
);
CREATE TABLE IF NOT EXISTS alarm_signal_state (
 tenant_id text NOT NULL,device_id text NOT NULL,signal_key text NOT NULL,event_at bigint NOT NULL,active boolean NOT NULL,observation_id text NOT NULL,
 PRIMARY KEY(tenant_id,device_id,signal_key)
);
CREATE TABLE IF NOT EXISTS alarm_governance_source_version (
 tenant_id text NOT NULL,dependency_key text NOT NULL,bucket_start bigint NOT NULL,generation bigint NOT NULL DEFAULT 0 CHECK(generation>=0),
 PRIMARY KEY(tenant_id,dependency_key,bucket_start)
);
CREATE INDEX IF NOT EXISTS alarm_observation_received ON alarm_observation(tenant_id,device_id,(COALESCE((body->>'receivedAt')::bigint,0)),id);
CREATE INDEX IF NOT EXISTS alarm_observation_evaluated ON alarm_observation(tenant_id,device_id,(COALESCE((body->>'evaluationAt')::bigint,0)),id);
