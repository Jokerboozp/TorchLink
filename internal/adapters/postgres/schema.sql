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
CREATE INDEX IF NOT EXISTS device_registry_parent_idx ON device_registry(tenant_id, (body->>'gatewayId'), id) WHERE body->>'gatewayId' IS NOT NULL;

-- Additive control-plane extensions; old device/product JSON remains valid.
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

CREATE TABLE IF NOT EXISTS onboarding_record (
 tenant_id text NOT NULL, id text NOT NULL, owner_id text NOT NULL,
 kind text NOT NULL, status text NOT NULL, revision bigint NOT NULL CHECK (revision > 0),
 created_at bigint NOT NULL, updated_at bigint NOT NULL, body jsonb NOT NULL,
 PRIMARY KEY (tenant_id,id)
);
CREATE INDEX IF NOT EXISTS onboarding_record_owner_kind ON onboarding_record(tenant_id,owner_id,kind,updated_at DESC,id);
CREATE INDEX IF NOT EXISTS onboarding_record_kind ON onboarding_record(tenant_id,kind,updated_at DESC,id);
CREATE INDEX IF NOT EXISTS onboarding_record_pending ON onboarding_record(kind,updated_at,id) WHERE status IN ('INITIALIZING','QUEUED','RUNNING');

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
-- one object storage JSONL artifact.
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
CREATE INDEX IF NOT EXISTS alarm_device_time_idx ON alarm_record(tenant_id, device_id, last_triggered_at DESC);
-- Transactional outbox: bus events committed with the alarm change that produced them.
CREATE TABLE IF NOT EXISTS event_outbox (
  seq bigserial PRIMARY KEY,
  topic text NOT NULL,
  event_key text NOT NULL,
  payload bytea NOT NULL
);

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
-- 告警研判按知识范围分开保存：'' 为未使用知识库的结果，其他值仅对有知识库权限的角色可见。
-- 迁移前的结果曾检索整个租户的知识库，统一标记为 legacy-tenant-knowledge，避免被无知识库权限的角色看到。
ALTER TABLE alarm_ai_analysis ADD COLUMN IF NOT EXISTS knowledge_scope text;
UPDATE alarm_ai_analysis SET knowledge_scope='legacy-tenant-knowledge' WHERE knowledge_scope IS NULL;
ALTER TABLE alarm_ai_analysis ALTER COLUMN knowledge_scope SET DEFAULT '';
ALTER TABLE alarm_ai_analysis ALTER COLUMN knowledge_scope SET NOT NULL;
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid='alarm_ai_analysis'::regclass
      AND conname='alarm_ai_analysis_pkey'
      AND array_length(conkey, 1)=2
  ) THEN
    ALTER TABLE alarm_ai_analysis DROP CONSTRAINT alarm_ai_analysis_pkey;
    ALTER TABLE alarm_ai_analysis ADD CONSTRAINT alarm_ai_analysis_pkey PRIMARY KEY(tenant_id, alarm_id, knowledge_scope);
  END IF;
END $$;
-- 智能巡检任务的进度与结果：服务重启后可继续读取，多个 API 副本共享；每个租户同时最多一个运行中的任务。
CREATE TABLE IF NOT EXISTS health_inspection_job (
  tenant_id text NOT NULL, id text NOT NULL, status text NOT NULL,
  started_at bigint NOT NULL, updated_at bigint NOT NULL, body jsonb NOT NULL,
  PRIMARY KEY(tenant_id, id)
);
CREATE UNIQUE INDEX IF NOT EXISTS health_inspection_job_one_running ON health_inspection_job(tenant_id) WHERE status='running';
CREATE INDEX IF NOT EXISTS health_inspection_job_latest ON health_inspection_job(tenant_id, started_at DESC);
CREATE TABLE IF NOT EXISTS alarm_analysis_job (
  tenant_id text NOT NULL, id text NOT NULL, alarm_id text NOT NULL, knowledge_scope text NOT NULL DEFAULT '',
  status text NOT NULL, started_at bigint NOT NULL, updated_at bigint NOT NULL, body jsonb NOT NULL,
  PRIMARY KEY(tenant_id, id)
);
CREATE UNIQUE INDEX IF NOT EXISTS alarm_analysis_job_one_running ON alarm_analysis_job(tenant_id, alarm_id, knowledge_scope) WHERE status='running';
CREATE INDEX IF NOT EXISTS alarm_analysis_job_latest ON alarm_analysis_job(tenant_id, alarm_id, knowledge_scope, started_at DESC);
CREATE TABLE IF NOT EXISTS ai_knowledge_doc (
  id text PRIMARY KEY, tenant_id text NOT NULL, workflow_id text NOT NULL DEFAULT '', product_id text, category text, tags text[] NOT NULL DEFAULT '{}', object_bucket text NOT NULL,
  object_key text NOT NULL, filename text NOT NULL, status text NOT NULL, metadata jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE ai_knowledge_doc ADD COLUMN IF NOT EXISTS category text;
ALTER TABLE ai_knowledge_doc ADD COLUMN IF NOT EXISTS tags text[] NOT NULL DEFAULT '{}';
ALTER TABLE ai_knowledge_doc ADD COLUMN IF NOT EXISTS workflow_id text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS ai_knowledge_doc_workflow_idx ON ai_knowledge_doc(tenant_id, workflow_id, created_at DESC);

-- Knowledge vectors share the platform database. Versions are replaced only
-- after a complete rebuild; failed builds never clear the current index.
CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public;
CREATE TABLE IF NOT EXISTS ai_knowledge_index_version (
  id text PRIMARY KEY, signature text NOT NULL, provider text NOT NULL,
  model text NOT NULL, dimensions integer NOT NULL CHECK (dimensions BETWEEN 1 AND 2000),
  preprocessing text NOT NULL,
  status text NOT NULL CHECK (status IN ('building','active','retired','failed')),
  created_at timestamptz NOT NULL DEFAULT now(), activated_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS ai_knowledge_one_active_version ON ai_knowledge_index_version ((status)) WHERE status='active';
CREATE INDEX IF NOT EXISTS ai_knowledge_index_signature ON ai_knowledge_index_version(signature, status);
CREATE TABLE IF NOT EXISTS ai_knowledge_chunk (
  version_id text NOT NULL REFERENCES ai_knowledge_index_version(id) ON DELETE CASCADE,
  tenant_id text NOT NULL, workflow_id text NOT NULL DEFAULT '',
  document_id text NOT NULL REFERENCES ai_knowledge_doc(id) ON DELETE CASCADE,
  chunk_id text NOT NULL, product_id text NOT NULL DEFAULT '', category text NOT NULL DEFAULT '',
  tags text[] NOT NULL DEFAULT '{}', chunk_index integer NOT NULL,
  start_char integer NOT NULL, end_char integer NOT NULL, character_count integer NOT NULL,
  overlap_chars integer NOT NULL DEFAULT 0, content text NOT NULL,
  keyword_tokens text[] NOT NULL DEFAULT '{}', dimensions integer NOT NULL,
  embedding public.vector NOT NULL,
  PRIMARY KEY (version_id, tenant_id, workflow_id, document_id, chunk_id),
  CHECK (start_char >= 0 AND end_char > start_char AND end_char-start_char=character_count),
  CHECK (overlap_chars >= 0 AND overlap_chars <= character_count),
  CHECK (public.vector_dims(embedding)=dimensions)
);
CREATE INDEX IF NOT EXISTS ai_knowledge_chunk_scope ON ai_knowledge_chunk(version_id,tenant_id,workflow_id,lower(product_id),lower(category));
CREATE INDEX IF NOT EXISTS ai_knowledge_chunk_document ON ai_knowledge_chunk(tenant_id,document_id);
CREATE INDEX IF NOT EXISTS ai_knowledge_chunk_tags ON ai_knowledge_chunk USING gin(tags);
CREATE INDEX IF NOT EXISTS ai_knowledge_chunk_keywords ON ai_knowledge_chunk USING gin(keyword_tokens);
-- Cosine HNSW expression indexes are created for each version/dimension by
-- the adapter, so vectors from different models never share a search index.

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


CREATE TABLE IF NOT EXISTS raw_ingest_reservation (
 tenant_id text NOT NULL,
 message_id text NOT NULL,
 payload_hash text NOT NULL,
 metadata jsonb NOT NULL,
 PRIMARY KEY(tenant_id,message_id)
);

-- Per component/type watermark and lifecycle, committed with alarm_record.
CREATE TABLE IF NOT EXISTS component_alarm_state (
  tenant_id text NOT NULL, device_id text NOT NULL, rule_id text NOT NULL,
  body jsonb NOT NULL,
  PRIMARY KEY (tenant_id, device_id, rule_id)
);
-- Access control (users, roles, API keys, device grants) lives in the
-- relational tables of migration 0005; the former platform_access document
-- table is renamed to platform_access_legacy by migration 0006.

-- Fire safety records live in fire_safety_record (migration 0007); the former
-- platform_fire_safety document table is renamed by migration 0008.

-- Ops center preferences are private to one account in one tenant.
CREATE TABLE IF NOT EXISTS ops_user_item (
 tenant_id text NOT NULL,
 username text NOT NULL,
 kind text NOT NULL,
 id text NOT NULL,
 name text NOT NULL DEFAULT '',
 body jsonb NOT NULL DEFAULT '{}'::jsonb,
 created_at bigint NOT NULL,
 updated_at bigint NOT NULL,
 PRIMARY KEY (tenant_id, username, kind, id)
);
CREATE INDEX IF NOT EXISTS idx_ops_user_item_recent ON ops_user_item(tenant_id, username, kind, updated_at DESC);

-- Platform connection fields moved from device tags to top-level body keys.
-- Existing top-level values win; the statement is idempotent.
UPDATE device_registry SET body = (body || jsonb_strip_nulls(jsonb_build_object(
  'connector', COALESCE(body->'connector', body->'tags'->'connector'),
  'connectorProfileId', COALESCE(body->'connectorProfileId', body->'tags'->'connectorProfileId'),
  'childAddress', COALESCE(body->'childAddress', body->'tags'->'childAddress'),
  'childType', COALESCE(body->'childType', body->'tags'->'childType'),
  'onboardingRequestHash', COALESCE(body->'onboardingRequestHash', body->'tags'->'onboardingRequestHash'))))
  || jsonb_build_object('tags', (body->'tags') - 'connector' - 'connectorProfileId' - 'childAddress' - 'childType' - 'onboardingRequestHash')
WHERE jsonb_typeof(body->'tags') = 'object'
  AND body->'tags' ?| ARRAY['connector', 'connectorProfileId', 'childAddress', 'childType', 'onboardingRequestHash'];

-- Device roles are stored explicitly; rows saved before the role existed get the
-- role their template category implied. The statement is idempotent.
UPDATE device_registry d SET body = jsonb_set(d.body, '{deviceRole}', to_jsonb(CASE
  WHEN COALESCE(d.body->>'gatewayId', '') <> '' THEN 'CHILD'
  WHEN EXISTS (SELECT 1 FROM iot_product p WHERE p.tenant_id = d.tenant_id AND p.id = d.product_id AND p.body->>'category' = 'gateway') THEN 'GATEWAY'
  ELSE 'DIRECT' END))
WHERE COALESCE(d.body->>'deviceRole', '') = '';

-- Every template on a versioned protocol has a binding, the single source of the
-- parsing version. Missing bindings are created from the template's protocol
-- reference; existing bindings are never changed.
INSERT INTO product_protocol_binding(tenant_id,product_id,protocol_id,version,body)
SELECT p.tenant_id, p.id, r.protocol_id, r.version,
  jsonb_build_object('tenantId', p.tenant_id, 'productId', p.id, 'protocolId', r.protocol_id, 'version', r.version, 'updatedAt', (extract(epoch FROM now()) * 1000)::bigint)
FROM iot_product p
LEFT JOIN protocol_package pkg ON pkg.tenant_id = p.tenant_id AND pkg.id = p.protocol_package_id
JOIN protocol_release r ON r.tenant_id = p.tenant_id AND r.status = 'PUBLISHED'
  AND ((r.protocol_id || '@' || r.version) = p.protocol_package_id
    OR (pkg.body->>'parserType' = 'go_protocol_parser' AND r.protocol_id = pkg.body->>'protocol' AND r.version = pkg.body->>'version'))
WHERE p.protocol_package_id <> 'iot-standard@1.0.0'
ON CONFLICT (tenant_id, product_id) DO NOTHING;

-- Optional camera live module. Live access settings, sealed credentials and
-- play sessions are kept apart from camera metadata (video_camera_mapping), so
-- saving a camera's name or location never touches live configuration.
CREATE TABLE IF NOT EXISTS video_module_state (
  id text PRIMARY KEY, enabled boolean NOT NULL DEFAULT false,
  updated_by text NOT NULL DEFAULT '', updated_at bigint NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS video_camera_live_config (
  tenant_id text NOT NULL, camera_id text NOT NULL, enabled boolean NOT NULL DEFAULT false,
  body jsonb NOT NULL, updated_at bigint NOT NULL,
  PRIMARY KEY (tenant_id, camera_id)
);
CREATE TABLE IF NOT EXISTS video_camera_credential (
  tenant_id text NOT NULL, camera_id text NOT NULL, key_id text NOT NULL,
  nonce bytea NOT NULL, ciphertext bytea NOT NULL, updated_at bigint NOT NULL,
  PRIMARY KEY (tenant_id, camera_id)
);
CREATE TABLE IF NOT EXISTS video_play_session (
  id text PRIMARY KEY, tenant_id text NOT NULL, camera_id text NOT NULL,
  expires_at bigint NOT NULL, revoked_at bigint NOT NULL DEFAULT 0, body jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS video_play_session_active_idx ON video_play_session(expires_at) WHERE revoked_at = 0;
-- GB28181 devices. device_id is the SIP identity and therefore global; body is
-- administrator-owned, state is written by the signalling server.
CREATE TABLE IF NOT EXISTS video_gb_device (
  device_id text PRIMARY KEY, tenant_id text NOT NULL, body jsonb NOT NULL,
  state jsonb NOT NULL DEFAULT '{}'::jsonb, key_id text NOT NULL DEFAULT '',
  nonce bytea, ciphertext bytea, updated_at bigint NOT NULL
);
CREATE INDEX IF NOT EXISTS video_gb_device_tenant_idx ON video_gb_device(tenant_id);

-- 巡检明细按不可变任务 ID 分页存储。迁移与元数据裁剪在 Migrate 的同一事务内。
CREATE TABLE IF NOT EXISTS health_inspection_item (
 tenant_id text NOT NULL, job_id text NOT NULL, position integer NOT NULL, body jsonb NOT NULL,
 PRIMARY KEY (tenant_id, job_id, position),
 FOREIGN KEY (tenant_id, job_id) REFERENCES health_inspection_job(tenant_id,id) ON DELETE CASCADE
);
INSERT INTO health_inspection_item(tenant_id,job_id,position,body)
SELECT j.tenant_id,j.id,(i.ordinality-1)::integer,i.value
FROM health_inspection_job j CROSS JOIN LATERAL jsonb_array_elements(
 CASE WHEN jsonb_typeof(j.body#>'{report,items}')='array' THEN j.body#>'{report,items}' ELSE '[]'::jsonb END
) WITH ORDINALITY i(value,ordinality)
ON CONFLICT DO NOTHING;
UPDATE health_inspection_job SET body=jsonb_set(body,'{report}',
 ((body->'report') - 'items') || jsonb_build_object('reportId',id,'totalItems',jsonb_array_length(body#>'{report,items}')))
WHERE jsonb_typeof(body#>'{report,items}')='array';

-- Cross-worker business processing: a message is claimed for a lease and only
-- the latest claim token may record completion (fencing). Alarms and device
-- states carry optimistic-concurrency versions for read-modify-write paths.
ALTER TABLE standard_message ADD COLUMN IF NOT EXISTS claim_owner text NOT NULL DEFAULT '';
ALTER TABLE standard_message ADD COLUMN IF NOT EXISTS claim_token bigint NOT NULL DEFAULT 0;
ALTER TABLE standard_message ADD COLUMN IF NOT EXISTS claim_expires_at bigint NOT NULL DEFAULT 0;
ALTER TABLE standard_message ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 0;
ALTER TABLE alarm_record ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 0;
ALTER TABLE device_state ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 0;

-- Durable external HTTP ingestion configuration, receipts and scheduled jobs.
-- Revision fences expired workers; claims always use the primary database.
CREATE TABLE IF NOT EXISTS external_data_entry (
 tenant_id text NOT NULL, kind text NOT NULL, id text NOT NULL,
 source_id text NOT NULL DEFAULT '', endpoint_id text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT '', due_at bigint NOT NULL DEFAULT 0,
 lease_until bigint NOT NULL DEFAULT 0, owner text NOT NULL DEFAULT '',
 revision bigint NOT NULL CHECK (revision > 0),
 created_at bigint NOT NULL, updated_at bigint NOT NULL, body jsonb NOT NULL,
 PRIMARY KEY (tenant_id, kind, id)
);
CREATE INDEX IF NOT EXISTS external_data_entry_list_idx ON external_data_entry(tenant_id,kind,created_at DESC,id);
CREATE INDEX IF NOT EXISTS external_data_entry_source_idx ON external_data_entry(tenant_id,kind,source_id,created_at DESC,id);
CREATE INDEX IF NOT EXISTS external_data_entry_endpoint_idx ON external_data_entry(tenant_id,kind,endpoint_id,status,created_at DESC,id);
CREATE INDEX IF NOT EXISTS external_data_entry_status_idx ON external_data_entry(tenant_id,kind,status,created_at DESC,id);
CREATE INDEX IF NOT EXISTS external_data_entry_job_idx ON external_data_entry(tenant_id,kind,(body->>'jobId'),status);
CREATE INDEX IF NOT EXISTS external_data_entry_updated_idx ON external_data_entry(tenant_id,kind,source_id,endpoint_id,status,updated_at DESC,id);
CREATE INDEX IF NOT EXISTS external_data_entry_pending_idx ON external_data_entry(kind,due_at,created_at,tenant_id,id) WHERE status IN ('PENDING','RETRY');
CREATE INDEX IF NOT EXISTS external_data_entry_running_idx ON external_data_entry(kind,lease_until,due_at) WHERE status='RUNNING';

-- Tenant overrides for externally published message topics, updated with CAS.
CREATE TABLE IF NOT EXISTS message_topic_configs (
 tenant_id text PRIMARY KEY,
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 body jsonb NOT NULL
);
-- Object storage files whose records are gone; a Jobs task deletes them.
CREATE TABLE IF NOT EXISTS object_cleanup (
 bucket text NOT NULL,
 object_key text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (bucket, object_key)
);
