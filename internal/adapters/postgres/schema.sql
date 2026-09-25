-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS iot_product (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, id text NOT NULL, status text NOT NULL,
  -- 继续当前数据库语句。
  protocol_package_id text, body jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS protocol_package (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, id text NOT NULL, status text NOT NULL,
  -- 继续当前数据库语句。
  parser_type text NOT NULL, body jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS device_registry (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, id text NOT NULL, product_id text NOT NULL, status text NOT NULL,
  -- 继续当前数据库语句。
  access_key text NOT NULL UNIQUE, secret_hash text NOT NULL, body jsonb NOT NULL,
  -- 更新数据库记录。
  updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS device_registry_product_idx ON device_registry(tenant_id, product_id);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS device_registry_parent_idx ON device_registry(tenant_id, (body->>'gatewayId'), id) WHERE body->>'gatewayId' IS NOT NULL;

-- Additive control-plane extensions; old device/product JSON remains valid.
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS device_credential_revocation (
 -- 继续当前数据库语句。
 tenant_id text NOT NULL,id text NOT NULL,device_id text NOT NULL,status text NOT NULL,
 -- 继续当前数据库语句。
 body jsonb NOT NULL,PRIMARY KEY(tenant_id,id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS device_credential_revocation_pending_idx ON device_credential_revocation(status);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS device_command (
 -- 继续当前数据库语句。
 tenant_id text NOT NULL,id text NOT NULL,device_id text NOT NULL,status text NOT NULL,
 -- 创建数据库对象。
 created_at bigint NOT NULL,body jsonb NOT NULL,PRIMARY KEY(tenant_id,id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS device_command_device_idx ON device_command(tenant_id,device_id,created_at DESC);

-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS raw_archive_index (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, product_id text NOT NULL, device_id text NOT NULL,
  -- 继续当前数据库语句。
  message_id text NOT NULL, protocol text, payload_format text,
  -- 继续当前数据库语句。
  object_bucket text NOT NULL, object_key text NOT NULL, object_offset bigint NOT NULL DEFAULT 0,
  -- 继续当前数据库语句。
  payload_hash text NOT NULL, payload_size integer NOT NULL,
  -- 继续当前数据库语句。
  received_at bigint NOT NULL, archived_at bigint NOT NULL, published_at bigint NOT NULL DEFAULT 0,
  -- 继续当前数据库语句。
  publish_attempts integer NOT NULL DEFAULT 0, last_publish_error text NOT NULL DEFAULT '',
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, message_id)
-- 继续当前数据库语句。
);
-- 调整数据库结构。
ALTER TABLE raw_archive_index ADD COLUMN IF NOT EXISTS published_at bigint NOT NULL DEFAULT 0;
-- 调整数据库结构。
ALTER TABLE raw_archive_index ADD COLUMN IF NOT EXISTS publish_attempts integer NOT NULL DEFAULT 0;
-- 调整数据库结构。
ALTER TABLE raw_archive_index ADD COLUMN IF NOT EXISTS last_publish_error text NOT NULL DEFAULT '';
-- 调整数据库结构。
ALTER TABLE raw_archive_index ADD COLUMN IF NOT EXISTS parse_attempted_at bigint NOT NULL DEFAULT 0;
-- 调整数据库结构。
ALTER TABLE raw_archive_index ADD COLUMN IF NOT EXISTS parse_error text NOT NULL DEFAULT '';
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS raw_archive_device_time_idx ON raw_archive_index(tenant_id, device_id, received_at DESC);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS raw_archive_product_time_idx ON raw_archive_index(tenant_id, product_id, received_at DESC);

-- Low-frequency raw payloads stay in PostgreSQL during the day. The daily
-- backup worker exports this table together with ClickHouse raw payloads to
-- one MinIO JSONL artifact.
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS raw_message_log (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, message_id text NOT NULL, product_id text NOT NULL,
  -- 继续当前数据库语句。
  device_id text NOT NULL, protocol text, payload_format text,
  -- 继续当前数据库语句。
  payload_hash text NOT NULL, payload_size integer NOT NULL,
  -- 继续当前数据库语句。
  received_at bigint NOT NULL, stored_at bigint NOT NULL, body jsonb NOT NULL,
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, message_id)
-- 继续当前数据库语句。
);

-- Protocol v2 keeps the protocol family stable while every release and point
-- table version is immutable. Product bindings are switched atomically and
-- retain the previous version for one-click rollback.
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS protocol_definition (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, id text NOT NULL, body jsonb NOT NULL,
  -- 更新数据库记录。
  updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id,id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS protocol_release (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, protocol_id text NOT NULL, version text NOT NULL,
  -- 继续当前数据库语句。
  status text NOT NULL, parser_type text NOT NULL, body jsonb NOT NULL,
  -- 创建数据库对象。
  created_at timestamptz NOT NULL DEFAULT now(),
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id,protocol_id,version)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS protocol_release_status_idx ON protocol_release(tenant_id,status,created_at DESC);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS point_table_release (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, protocol_id text NOT NULL, version text NOT NULL,
  -- 继续当前数据库语句。
  source_sha256 text NOT NULL DEFAULT '', body jsonb NOT NULL,
  -- 创建数据库对象。
  created_at timestamptz NOT NULL DEFAULT now(),
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id,protocol_id,version)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS product_protocol_binding (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, product_id text NOT NULL, protocol_id text NOT NULL,
  -- 继续当前数据库语句。
  version text NOT NULL, body jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id,product_id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS device_access_profile (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, id text NOT NULL, device_id text NOT NULL,
  -- 继续当前数据库语句。
  product_id text NOT NULL, enabled boolean NOT NULL, body jsonb NOT NULL,
  -- 更新数据库记录。
  updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id,id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS device_access_profile_runtime_idx ON device_access_profile(enabled,tenant_id,device_id);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS raw_message_log_device_time_idx ON raw_message_log(tenant_id, device_id, received_at DESC);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS raw_message_log_time_idx ON raw_message_log(received_at, message_id);

-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS standard_message (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, message_id text NOT NULL, raw_message_id text NOT NULL,
  -- 继续当前数据库语句。
  product_id text NOT NULL, device_id text NOT NULL, message_type text NOT NULL,
  -- 继续当前数据库语句。
  ts bigint NOT NULL, properties jsonb NOT NULL DEFAULT '{}', event jsonb NOT NULL DEFAULT '{}',
  -- 继续当前数据库语句。
  tags jsonb NOT NULL DEFAULT '{}', body jsonb NOT NULL, processed_at bigint NOT NULL DEFAULT 0,
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, message_id)
-- 继续当前数据库语句。
);
-- 调整数据库结构。
ALTER TABLE standard_message ADD COLUMN IF NOT EXISTS processed_at bigint NOT NULL DEFAULT 0;
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS standard_message_device_time_idx ON standard_message(tenant_id, device_id, ts DESC);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS standard_message_raw_idx ON standard_message(tenant_id, raw_message_id);

-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS device_state (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, device_id text NOT NULL, product_id text NOT NULL,
  -- 继续当前数据库语句。
  business_status text NOT NULL, last_seen_at bigint NOT NULL DEFAULT 0, body jsonb NOT NULL,
  -- 更新数据库记录。
  updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, device_id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS device_state_status_idx ON device_state(tenant_id, business_status);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS device_state_event (
  -- 继续当前数据库语句。
  id bigserial PRIMARY KEY, tenant_id text NOT NULL, device_id text NOT NULL,
  -- 继续当前数据库语句。
  business_status text NOT NULL, body jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
-- 继续当前数据库语句。
);

-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS alarm_rule (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, id text NOT NULL, product_id text, enabled boolean NOT NULL,
  -- 继续当前数据库语句。
  body jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS alarm_rule_pending (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, rule_id text NOT NULL, device_id text NOT NULL,
  -- 继续当前数据库语句。
  since_at bigint NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, rule_id, device_id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS alarm_record (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, id text NOT NULL, rule_id text NOT NULL, device_id text NOT NULL,
  -- 继续当前数据库语句。
  status text NOT NULL, level text NOT NULL, source text NOT NULL,
  -- 继续当前数据库语句。
  last_triggered_at bigint NOT NULL, body jsonb NOT NULL,
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE UNIQUE INDEX IF NOT EXISTS alarm_active_dedup_idx ON alarm_record(tenant_id, device_id, rule_id) WHERE status IN ('ACTIVE','ACKED');
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS alarm_query_idx ON alarm_record(tenant_id, status, last_triggered_at DESC);

-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS video_alarm_event (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, event_id text NOT NULL, camera_id text NOT NULL,
  -- 继续当前数据库语句。
  alarm_type text NOT NULL, event_time bigint NOT NULL, body jsonb NOT NULL,
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, event_id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS video_camera_mapping (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, camera_id text NOT NULL, camera_name text, brand text, camera_point text, device_id text, project_id text,
  -- 继续当前数据库语句。
  ingest_mode text NOT NULL DEFAULT 'direct', city_code text, district_code text, building text, floor text, room text, area_id text,
  -- 继续当前数据库语句。
  related_device_ids jsonb NOT NULL DEFAULT '[]', related_floor_ids jsonb NOT NULL DEFAULT '[]', related_room_ids jsonb NOT NULL DEFAULT '[]',
  -- 继续当前数据库语句。
  video_platform_id text, stream_url text, stream_type text, sdk_endpoint text, sdk_camera_id text, sdk_credential_ref text,
  -- 继续当前数据库语句。
  enabled boolean NOT NULL DEFAULT true,
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, camera_id)
-- 继续当前数据库语句。
);
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS ingest_mode text NOT NULL DEFAULT 'direct';
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS brand text;
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS camera_point text;
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS device_id text;
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS room text;
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS city_code text;
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS district_code text;
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS stream_type text;
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS related_floor_ids jsonb NOT NULL DEFAULT '[]';
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS related_room_ids jsonb NOT NULL DEFAULT '[]';
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS sdk_endpoint text;
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS sdk_camera_id text;
-- 调整数据库结构。
ALTER TABLE video_camera_mapping ADD COLUMN IF NOT EXISTS sdk_credential_ref text;
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS video_camera_relation (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, camera_id text NOT NULL, relation_type text NOT NULL,
  -- 继续当前数据库语句。
  target_id text NOT NULL,
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, camera_id, relation_type, target_id),
  -- 继续当前数据库语句。
  CHECK (relation_type IN ('device','floor','room'))
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS video_camera_relation_target_idx ON video_camera_relation(tenant_id, relation_type, target_id);
-- 写入数据库记录。
INSERT INTO video_camera_relation(tenant_id,camera_id,relation_type,target_id)
-- 查询数据库记录。
SELECT tenant_id,camera_id,'device',jsonb_array_elements_text(coalesce(related_device_ids,'[]'::jsonb))
-- 指定数据来源。
FROM video_camera_mapping
-- 继续当前数据库语句。
ON CONFLICT DO NOTHING;
-- 写入数据库记录。
INSERT INTO video_camera_relation(tenant_id,camera_id,relation_type,target_id)
-- 查询数据库记录。
SELECT tenant_id,camera_id,'floor',jsonb_array_elements_text(coalesce(related_floor_ids,'[]'::jsonb))
-- 指定数据来源。
FROM video_camera_mapping
-- 继续当前数据库语句。
ON CONFLICT DO NOTHING;
-- 写入数据库记录。
INSERT INTO video_camera_relation(tenant_id,camera_id,relation_type,target_id)
-- 查询数据库记录。
SELECT tenant_id,camera_id,'room',jsonb_array_elements_text(coalesce(related_room_ids,'[]'::jsonb))
-- 指定数据来源。
FROM video_camera_mapping
-- 继续当前数据库语句。
ON CONFLICT DO NOTHING;
-- Cameras are now associated with at most one device. Keep the first legacy
-- association during migration, then enforce the cardinality with a unique
-- partial index. Floor/room JSON columns remain only as legacy storage.
-- 删除数据库记录。
DELETE FROM video_camera_relation relation
-- 限制操作条件。
WHERE relation.relation_type = 'device'
  -- 继续当前数据库语句。
  AND relation.target_id <> (
    -- 查询数据库记录。
    SELECT MIN(candidate.target_id)
    -- 指定数据来源。
    FROM video_camera_relation candidate
    -- 限制操作条件。
    WHERE candidate.tenant_id = relation.tenant_id
      -- 继续当前数据库语句。
      AND candidate.camera_id = relation.camera_id
      -- 继续当前数据库语句。
      AND candidate.relation_type = 'device'
  -- 继续当前数据库语句。
  );
-- 更新数据库记录。
UPDATE video_camera_mapping mapping
-- 继续当前数据库语句。
SET device_id = relation.target_id
-- 指定数据来源。
FROM video_camera_relation relation
-- 限制操作条件。
WHERE relation.tenant_id = mapping.tenant_id
  -- 继续当前数据库语句。
  AND relation.camera_id = mapping.camera_id
  -- 继续当前数据库语句。
  AND relation.relation_type = 'device'
  -- 继续当前数据库语句。
  AND (mapping.device_id IS NULL OR mapping.device_id = '');
-- 创建数据库对象。
CREATE UNIQUE INDEX IF NOT EXISTS video_camera_relation_camera_device_unique_idx
  -- 继续当前数据库语句。
  ON video_camera_relation(tenant_id, camera_id) WHERE relation_type = 'device';
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS video_alarm_media (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, event_id text NOT NULL, media_type text NOT NULL,
  -- 继续当前数据库语句。
  object_bucket text NOT NULL, object_key text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, event_id, media_type, object_key)
-- 继续当前数据库语句。
);

-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS ai_model_config (
  -- 继续当前数据库语句。
  id text PRIMARY KEY, tenant_id text NOT NULL, provider text NOT NULL, model text NOT NULL,
  -- 继续当前数据库语句。
  config jsonb NOT NULL DEFAULT '{}', enabled boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT now()
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS ai_prompt_template (
  -- 继续当前数据库语句。
  id text NOT NULL, version text NOT NULL, tenant_id text NOT NULL, content text NOT NULL,
  -- 继续当前数据库语句。
  enabled boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(id, version, tenant_id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS alarm_ai_analysis (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, alarm_id text NOT NULL, body jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  -- 继续当前数据库语句。
  PRIMARY KEY(tenant_id, alarm_id)
-- 继续当前数据库语句。
);
-- 调整数据库结构。
ALTER TABLE alarm_ai_analysis ADD COLUMN IF NOT EXISTS tenant_id text;
-- 继续当前数据库语句。
WITH unambiguous_alarm_owner AS (
  -- 查询数据库记录。
  SELECT id AS alarm_id, min(tenant_id) AS tenant_id
  -- 指定数据来源。
  FROM alarm_record
  -- 继续当前数据库语句。
  GROUP BY id
  -- 继续当前数据库语句。
  HAVING count(*)=1
-- 继续当前数据库语句。
)
-- 更新数据库记录。
UPDATE alarm_ai_analysis AS analysis
-- 继续当前数据库语句。
SET tenant_id=owner.tenant_id
-- 指定数据来源。
FROM unambiguous_alarm_owner AS owner
-- 限制操作条件。
WHERE analysis.tenant_id IS NULL AND analysis.alarm_id=owner.alarm_id;
-- Preserve unmatched or ambiguous legacy rows without exposing them to a real tenant.
-- 更新数据库记录。
UPDATE alarm_ai_analysis SET tenant_id='__legacy_orphaned__' WHERE tenant_id IS NULL;
-- 调整数据库结构。
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
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS ai_knowledge_doc (
  -- 继续当前数据库语句。
  id text PRIMARY KEY, tenant_id text NOT NULL, workflow_id text NOT NULL DEFAULT '', product_id text, category text, tags text[] NOT NULL DEFAULT '{}', object_bucket text NOT NULL,
  -- 继续当前数据库语句。
  object_key text NOT NULL, filename text NOT NULL, status text NOT NULL, metadata jsonb NOT NULL DEFAULT '{}',
  -- 创建数据库对象。
  created_at timestamptz NOT NULL DEFAULT now()
-- 继续当前数据库语句。
);
-- 调整数据库结构。
ALTER TABLE ai_knowledge_doc ADD COLUMN IF NOT EXISTS category text;
-- 调整数据库结构。
ALTER TABLE ai_knowledge_doc ADD COLUMN IF NOT EXISTS tags text[] NOT NULL DEFAULT '{}';
-- 调整数据库结构。
ALTER TABLE ai_knowledge_doc ADD COLUMN IF NOT EXISTS workflow_id text NOT NULL DEFAULT '';
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS ai_knowledge_doc_workflow_idx ON ai_knowledge_doc(tenant_id, workflow_id, created_at DESC);

-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS ai_workflow_knowledge_binding (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL,
  -- 继续当前数据库语句。
  workflow_id text NOT NULL,
  -- 继续当前数据库语句。
  body jsonb NOT NULL,
  -- 更新数据库记录。
  updated_at timestamptz NOT NULL DEFAULT now(),
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, workflow_id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS ai_tool_call_log (
  -- 继续当前数据库语句。
  id bigserial PRIMARY KEY, tenant_id text NOT NULL, actor text, tool text NOT NULL,
  -- 继续当前数据库语句。
  trace_id text, input jsonb, output jsonb, success boolean NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
-- 继续当前数据库语句。
);
-- 调整数据库结构。
ALTER TABLE ai_tool_call_log ADD COLUMN IF NOT EXISTS trace_id text;
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS ai_tool_call_trace_idx ON ai_tool_call_log(tenant_id, trace_id) WHERE trace_id IS NOT NULL;

-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS replay_task (
  -- 继续当前数据库语句。
  id text PRIMARY KEY, tenant_id text NOT NULL, status text NOT NULL, body jsonb NOT NULL,
  -- 创建数据库对象。
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS backup_task (
  -- 继续当前数据库语句。
  id text PRIMARY KEY, backup_type text NOT NULL, status text NOT NULL, object_key text,
  -- 继续当前数据库语句。
  checksum text, details jsonb NOT NULL DEFAULT '{}', started_at timestamptz, completed_at timestamptz
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS backup_task_history_idx ON backup_task (completed_at DESC NULLS LAST, started_at DESC);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS audit_log (
  -- 继续当前数据库语句。
  id text PRIMARY KEY, tenant_id text NOT NULL, actor text NOT NULL, action text NOT NULL,
  -- 继续当前数据库语句。
  target_type text NOT NULL, target_id text NOT NULL, details jsonb NOT NULL DEFAULT '{}',
  -- 创建数据库对象。
  created_at bigint NOT NULL
-- 继续当前数据库语句。
);

-- 创建数据库对象。
CREATE INDEX IF NOT EXISTS device_state_event_device_idx ON device_state_event(tenant_id,device_id,id DESC);

-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS execution_lease (
 -- 继续当前数据库语句。
 tenant_id text NOT NULL,
 -- 继续当前数据库语句。
 resource text NOT NULL,
 -- 继续当前数据库语句。
 owner text NOT NULL,
 -- 继续当前数据库语句。
 endpoint text NOT NULL DEFAULT '',
 -- 继续当前数据库语句。
 token bigint NOT NULL DEFAULT 1,
 -- 继续当前数据库语句。
 expires_at timestamptz NOT NULL,
 -- 继续当前数据库语句。
 PRIMARY KEY(tenant_id,resource)
-- 继续当前数据库语句。
);


-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS raw_ingest_reservation (
 -- 继续当前数据库语句。
 tenant_id text NOT NULL,
 -- 继续当前数据库语句。
 message_id text NOT NULL,
 -- 继续当前数据库语句。
 payload_hash text NOT NULL,
 -- 继续当前数据库语句。
 metadata jsonb NOT NULL,
 -- 继续当前数据库语句。
 PRIMARY KEY(tenant_id,message_id)
-- 继续当前数据库语句。
);

-- Per component/type watermark and lifecycle, committed with alarm_record.
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS component_alarm_state (
  -- 继续当前数据库语句。
  tenant_id text NOT NULL, device_id text NOT NULL, rule_id text NOT NULL,
  -- 继续当前数据库语句。
  body jsonb NOT NULL,
  -- 继续当前数据库语句。
  PRIMARY KEY (tenant_id, device_id, rule_id)
-- 继续当前数据库语句。
);
-- 创建数据库对象。
CREATE TABLE IF NOT EXISTS platform_access (
 -- 继续当前数据库语句。
 tenant_id text PRIMARY KEY,
 -- 继续当前数据库语句。
 revision bigint NOT NULL DEFAULT 1,
 -- 继续当前数据库语句。
 body jsonb NOT NULL
-- 继续当前数据库语句。
);

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
