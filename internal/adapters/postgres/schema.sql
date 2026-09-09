CREATE TABLE IF NOT EXISTS iot_product (
  tenant_id text NOT NULL, id text NOT NULL, status text NOT NULL,
  protocol_package_id text, body jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id)
);
CREATE TABLE IF NOT EXISTS protocol_package (
  tenant_id text NOT NULL, id text NOT NULL, status text NOT NULL,
  parser_type text NOT NULL, body jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id)
);
CREATE TABLE IF NOT EXISTS device_registry (
  tenant_id text NOT NULL, id text NOT NULL, product_id text NOT NULL, status text NOT NULL,
  access_key text NOT NULL UNIQUE, secret_hash text NOT NULL, body jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, id)
);
CREATE INDEX IF NOT EXISTS device_registry_product_idx ON device_registry(tenant_id, product_id);

-- Additive control-plane extensions; old device/product JSON remains valid.
CREATE TABLE IF NOT EXISTS edge_node (
 tenant_id text NOT NULL,id text NOT NULL,body jsonb NOT NULL,
 PRIMARY KEY(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS device_credential_revocation (
 tenant_id text NOT NULL,id text NOT NULL,device_id text NOT NULL,status text NOT NULL,
 body jsonb NOT NULL,PRIMARY KEY(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS device_credential_revocation_pending_idx ON device_credential_revocation(status);
CREATE TABLE IF NOT EXISTS device_command (
 tenant_id text NOT NULL,id text NOT NULL,device_id text NOT NULL,status text NOT NULL,
 created_at bigint NOT NULL,body jsonb NOT NULL,PRIMARY KEY(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS device_command_device_idx ON device_command(tenant_id,device_id,created_at DESC);

CREATE TABLE IF NOT EXISTS raw_archive_index (
  tenant_id text NOT NULL, product_id text NOT NULL, device_id text NOT NULL,
  message_id text NOT NULL, protocol text, payload_format text,
  object_bucket text NOT NULL, object_key text NOT NULL, object_offset bigint NOT NULL DEFAULT 0,
  payload_hash text NOT NULL, payload_size integer NOT NULL,
  received_at bigint NOT NULL, archived_at bigint NOT NULL, published_at bigint NOT NULL DEFAULT 0,
  publish_attempts integer NOT NULL DEFAULT 0, last_publish_error text NOT NULL DEFAULT '',
  PRIMARY KEY (tenant_id, message_id)
);
ALTER TABLE raw_archive_index ADD COLUMN IF NOT EXISTS published_at bigint NOT NULL DEFAULT 0;
ALTER TABLE raw_archive_index ADD COLUMN IF NOT EXISTS publish_attempts integer NOT NULL DEFAULT 0;
ALTER TABLE raw_archive_index ADD COLUMN IF NOT EXISTS last_publish_error text NOT NULL DEFAULT '';
ALTER TABLE raw_archive_index ADD COLUMN IF NOT EXISTS parse_attempted_at bigint NOT NULL DEFAULT 0;
ALTER TABLE raw_archive_index ADD COLUMN IF NOT EXISTS parse_error text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS raw_archive_device_time_idx ON raw_archive_index(tenant_id, device_id, received_at DESC);
CREATE INDEX IF NOT EXISTS raw_archive_product_time_idx ON raw_archive_index(tenant_id, product_id, received_at DESC);

-- Low-frequency raw payloads stay in PostgreSQL during the day. The daily
-- backup worker exports this table together with ClickHouse raw payloads to
-- one MinIO JSONL artifact.
CREATE TABLE IF NOT EXISTS raw_message_log (
  tenant_id text NOT NULL, message_id text NOT NULL, product_id text NOT NULL,
  device_id text NOT NULL, protocol text, payload_format text,
  payload_hash text NOT NULL, payload_size integer NOT NULL,
  received_at bigint NOT NULL, stored_at bigint NOT NULL, body jsonb NOT NULL,
  PRIMARY KEY (tenant_id, message_id)
);

-- Protocol v2 keeps the protocol family stable while every release and point
-- table version is immutable. Product bindings are switched atomically and
-- retain the previous version for one-click rollback.
CREATE TABLE IF NOT EXISTS protocol_definition (
  tenant_id text NOT NULL, id text NOT NULL, body jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id,id)
);
CREATE TABLE IF NOT EXISTS protocol_release (
  tenant_id text NOT NULL, protocol_id text NOT NULL, version text NOT NULL,
  status text NOT NULL, parser_type text NOT NULL, body jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id,protocol_id,version)
);
CREATE INDEX IF NOT EXISTS protocol_release_status_idx ON protocol_release(tenant_id,status,created_at DESC);
CREATE TABLE IF NOT EXISTS point_table_release (
  tenant_id text NOT NULL, protocol_id text NOT NULL, version text NOT NULL,
  source_sha256 text NOT NULL DEFAULT '', body jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id,protocol_id,version)
);
CREATE TABLE IF NOT EXISTS product_protocol_binding (
  tenant_id text NOT NULL, product_id text NOT NULL, protocol_id text NOT NULL,
  version text NOT NULL, body jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id,product_id)
);
CREATE TABLE IF NOT EXISTS device_access_profile (
  tenant_id text NOT NULL, id text NOT NULL, device_id text NOT NULL,
  product_id text NOT NULL, enabled boolean NOT NULL, body jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id,id)
);
CREATE INDEX IF NOT EXISTS device_access_profile_runtime_idx ON device_access_profile(enabled,tenant_id,device_id);
CREATE INDEX IF NOT EXISTS raw_message_log_device_time_idx ON raw_message_log(tenant_id, device_id, received_at DESC);
CREATE INDEX IF NOT EXISTS raw_message_log_time_idx ON raw_message_log(received_at, message_id);

CREATE TABLE IF NOT EXISTS standard_message (
  tenant_id text NOT NULL, message_id text NOT NULL, raw_message_id text NOT NULL,
  product_id text NOT NULL, device_id text NOT NULL, message_type text NOT NULL,
  ts bigint NOT NULL, properties jsonb NOT NULL DEFAULT '{}', event jsonb NOT NULL DEFAULT '{}',
  tags jsonb NOT NULL DEFAULT '{}', body jsonb NOT NULL, processed_at bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (tenant_id, message_id)
);
ALTER TABLE standard_message ADD COLUMN IF NOT EXISTS processed_at bigint NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS standard_message_device_time_idx ON standard_message(tenant_id, device_id, ts DESC);
CREATE INDEX IF NOT EXISTS standard_message_raw_idx ON standard_message(tenant_id, raw_message_id);

CREATE TABLE IF NOT EXISTS device_state (
  tenant_id text NOT NULL, device_id text NOT NULL, product_id text NOT NULL,
  business_status text NOT NULL, last_seen_at bigint NOT NULL DEFAULT 0, body jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, device_id)
);
CREATE INDEX IF NOT EXISTS device_state_status_idx ON device_state(tenant_id, business_status);
CREATE TABLE IF NOT EXISTS device_state_event (
  id bigserial PRIMARY KEY, tenant_id text NOT NULL, device_id text NOT NULL,
  business_status text NOT NULL, body jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS alarm_rule (
  tenant_id text NOT NULL, id text NOT NULL, product_id text, enabled boolean NOT NULL,
  body jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, id)
);
CREATE TABLE IF NOT EXISTS alarm_rule_pending (
  tenant_id text NOT NULL, rule_id text NOT NULL, device_id text NOT NULL,
  since_at bigint NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, rule_id, device_id)
);
CREATE TABLE IF NOT EXISTS alarm_record (
  tenant_id text NOT NULL, id text NOT NULL, rule_id text NOT NULL, device_id text NOT NULL,
  status text NOT NULL, level text NOT NULL, source text NOT NULL,
  last_triggered_at bigint NOT NULL, body jsonb NOT NULL,
  PRIMARY KEY (tenant_id, id)
);
CREATE UNIQUE INDEX IF NOT EXISTS alarm_active_dedup_idx ON alarm_record(tenant_id, device_id, rule_id) WHERE status IN ('ACTIVE','ACKED');
CREATE INDEX IF NOT EXISTS alarm_query_idx ON alarm_record(tenant_id, status, last_triggered_at DESC);

CREATE TABLE IF NOT EXISTS video_alarm_event (
  tenant_id text NOT NULL, event_id text NOT NULL, camera_id text NOT NULL,
  alarm_type text NOT NULL, event_time bigint NOT NULL, body jsonb NOT NULL,
  PRIMARY KEY (tenant_id, event_id)
);
CREATE TABLE IF NOT EXISTS video_camera_mapping (
  tenant_id text NOT NULL, camera_id text NOT NULL, camera_name text, brand text, camera_point text, device_id text, project_id text,
  ingest_mode text NOT NULL DEFAULT 'direct', city_code text, district_code text, building text, floor text, room text, area_id text,
  related_device_ids jsonb NOT NULL DEFAULT '[]', related_floor_ids jsonb NOT NULL DEFAULT '[]', related_room_ids jsonb NOT NULL DEFAULT '[]',
  video_platform_id text, stream_url text, stream_type text, sdk_endpoint text, sdk_camera_id text, sdk_credential_ref text,
  enabled boolean NOT NULL DEFAULT true,
  PRIMARY KEY (tenant_id, camera_id)
);
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS ingest_mode text NOT NULL DEFAULT 'direct';
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS brand text;
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS camera_point text;
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS device_id text;
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS room text;
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS city_code text;
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS district_code text;
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS stream_type text;
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS related_floor_ids jsonb NOT NULL DEFAULT '[]';
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS related_room_ids jsonb NOT NULL DEFAULT '[]';
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS sdk_endpoint text;
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS sdk_camera_id text;
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS sdk_credential_ref text;
CREATE TABLE IF NOT EXISTS video_camera_relation (
  tenant_id text NOT NULL, camera_id text NOT NULL, relation_type text NOT NULL,
  target_id text NOT NULL,
  PRIMARY KEY (tenant_id, camera_id, relation_type, target_id),
  CHECK (relation_type IN ('device','floor','room'))
);
CREATE INDEX IF NOT EXISTS video_camera_relation_target_idx ON video_camera_relation(tenant_id, relation_type, target_id);
INSERT INTO video_camera_relation(tenant_id,camera_id,relation_type,target_id)
SELECT tenant_id,camera_id,'device',jsonb_array_elements_text(coalesce(related_device_ids,'[]'::jsonb))
FROM video_camera_mapping
ON CONFLICT DO NOTHING;
INSERT INTO video_camera_relation(tenant_id,camera_id,relation_type,target_id)
SELECT tenant_id,camera_id,'floor',jsonb_array_elements_text(coalesce(related_floor_ids,'[]'::jsonb))
FROM video_camera_mapping
ON CONFLICT DO NOTHING;
INSERT INTO video_camera_relation(tenant_id,camera_id,relation_type,target_id)
SELECT tenant_id,camera_id,'room',jsonb_array_elements_text(coalesce(related_room_ids,'[]'::jsonb))
FROM video_camera_mapping
ON CONFLICT DO NOTHING;
-- Cameras are now associated with at most one device. Keep the first legacy
-- association during migration, then enforce the cardinality with a unique
-- partial index. Floor/room JSON columns remain only as legacy storage.
DELETE FROM video_camera_relation relation
WHERE relation.relation_type = 'device'
  AND relation.target_id <> (
    SELECT MIN(candidate.target_id)
    FROM video_camera_relation candidate
    WHERE candidate.tenant_id = relation.tenant_id
      AND candidate.camera_id = relation.camera_id
      AND candidate.relation_type = 'device'
  );
UPDATE video_camera_mapping mapping
SET device_id = relation.target_id
FROM video_camera_relation relation
WHERE relation.tenant_id = mapping.tenant_id
  AND relation.camera_id = mapping.camera_id
  AND relation.relation_type = 'device'
  AND (mapping.device_id IS NULL OR mapping.device_id = '');
CREATE UNIQUE INDEX IF NOT EXISTS video_camera_relation_camera_device_unique_idx
  ON video_camera_relation(tenant_id, camera_id) WHERE relation_type = 'device';
CREATE TABLE IF NOT EXISTS video_alarm_media (
  tenant_id text NOT NULL, event_id text NOT NULL, media_type text NOT NULL,
  object_bucket text NOT NULL, object_key text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, event_id, media_type, object_key)
);

CREATE TABLE IF NOT EXISTS ai_model_config (
  id text PRIMARY KEY, tenant_id text NOT NULL, provider text NOT NULL, model text NOT NULL,
  config jsonb NOT NULL DEFAULT '{}', enabled boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS ai_prompt_template (
  id text NOT NULL, version text NOT NULL, tenant_id text NOT NULL, content text NOT NULL,
  enabled boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(id, version, tenant_id)
);
CREATE TABLE IF NOT EXISTS alarm_ai_analysis (
  tenant_id text NOT NULL, alarm_id text NOT NULL, body jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(tenant_id, alarm_id)
);
ALTER TABLE alarm_ai_analysis ADD COLUMN IF NOT EXISTS tenant_id text;
WITH unambiguous_alarm_owner AS (
  SELECT id AS alarm_id, min(tenant_id) AS tenant_id
  FROM alarm_record
  GROUP BY id
  HAVING count(*)=1
)
UPDATE alarm_ai_analysis AS analysis
SET tenant_id=owner.tenant_id
FROM unambiguous_alarm_owner AS owner
WHERE analysis.tenant_id IS NULL AND analysis.alarm_id=owner.alarm_id;
-- Preserve unmatched or ambiguous legacy rows without exposing them to a real tenant.
UPDATE alarm_ai_analysis SET tenant_id='__legacy_orphaned__' WHERE tenant_id IS NULL;
ALTER TABLE alarm_ai_analysis ALTER COLUMN tenant_id SET NOT NULL;
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid='alarm_ai_analysis'::regclass
      AND conname='alarm_ai_analysis_pkey'
      AND array_length(conkey, 1)=1
  ) THEN
    ALTER TABLE alarm_ai_analysis DROP CONSTRAINT alarm_ai_analysis_pkey;
    ALTER TABLE alarm_ai_analysis ADD CONSTRAINT alarm_ai_analysis_pkey PRIMARY KEY(tenant_id, alarm_id);
  END IF;
END $$;
CREATE TABLE IF NOT EXISTS ai_knowledge_doc (
  id text PRIMARY KEY, tenant_id text NOT NULL, workflow_id text NOT NULL DEFAULT '', product_id text, category text, tags text[] NOT NULL DEFAULT '{}', object_bucket text NOT NULL,
  object_key text NOT NULL, filename text NOT NULL, status text NOT NULL, metadata jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE ai_knowledge_doc ADD COLUMN IF NOT EXISTS category text;
ALTER TABLE ai_knowledge_doc ADD COLUMN IF NOT EXISTS tags text[] NOT NULL DEFAULT '{}';
ALTER TABLE ai_knowledge_doc ADD COLUMN IF NOT EXISTS workflow_id text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS ai_knowledge_doc_workflow_idx ON ai_knowledge_doc(tenant_id, workflow_id, created_at DESC);

CREATE TABLE IF NOT EXISTS ai_workflow_knowledge_binding (
  tenant_id text NOT NULL,
  workflow_id text NOT NULL,
  body jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, workflow_id)
);
CREATE TABLE IF NOT EXISTS ai_tool_call_log (
  id bigserial PRIMARY KEY, tenant_id text NOT NULL, actor text, tool text NOT NULL,
  trace_id text, input jsonb, output jsonb, success boolean NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE ai_tool_call_log ADD COLUMN IF NOT EXISTS trace_id text;
CREATE INDEX IF NOT EXISTS ai_tool_call_trace_idx ON ai_tool_call_log(tenant_id, trace_id) WHERE trace_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS replay_task (
  id text PRIMARY KEY, tenant_id text NOT NULL, status text NOT NULL, body jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS backup_task (
  id text PRIMARY KEY, backup_type text NOT NULL, status text NOT NULL, object_key text,
  checksum text, details jsonb NOT NULL DEFAULT '{}', started_at timestamptz, completed_at timestamptz
);
CREATE INDEX IF NOT EXISTS backup_task_history_idx ON backup_task (completed_at DESC NULLS LAST, started_at DESC);
CREATE TABLE IF NOT EXISTS audit_log (
  id text PRIMARY KEY, tenant_id text NOT NULL, actor text NOT NULL, action text NOT NULL,
  target_type text NOT NULL, target_id text NOT NULL, details jsonb NOT NULL DEFAULT '{}',
  created_at bigint NOT NULL
);

CREATE INDEX IF NOT EXISTS device_state_event_device_idx ON device_state_event(tenant_id,device_id,id DESC);

CREATE TABLE IF NOT EXISTS execution_lease (
 tenant_id text NOT NULL,
 resource text NOT NULL,
 owner text NOT NULL,
 endpoint text NOT NULL DEFAULT '',
 token bigint NOT NULL DEFAULT 1,
 expires_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,resource)
);

ALTER TABLE edge_node ADD COLUMN IF NOT EXISTS secret_hash text NOT NULL DEFAULT '';
ALTER TABLE edge_node ADD COLUMN IF NOT EXISTS heartbeat jsonb NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS raw_ingest_reservation (
 tenant_id text NOT NULL,
 message_id text NOT NULL,
 payload_hash text NOT NULL,
 metadata jsonb NOT NULL,
 PRIMARY KEY(tenant_id,message_id)
);
CREATE TABLE IF NOT EXISTS edge_read_job (
  tenant_id text NOT NULL,
  id text NOT NULL,
  node_id text NOT NULL,
  expires_at bigint NOT NULL,
  status text NOT NULL,
  body jsonb NOT NULL,
  PRIMARY KEY (tenant_id,id)
);
CREATE INDEX IF NOT EXISTS edge_read_job_pending_idx ON edge_read_job(tenant_id,node_id,status,expires_at);
