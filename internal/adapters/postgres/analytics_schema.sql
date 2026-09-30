-- Only compact analytics objects live here. Source time series stay in their
-- existing PostgreSQL/ClickHouse tables. Resource association and immutable
-- revisions are checked in the shared transactional store engine.
CREATE TABLE IF NOT EXISTS analysis_document (
  tenant_id text NOT NULL,
  kind text NOT NULL,
  id text NOT NULL,
  run_id text NOT NULL DEFAULT '',
  device_id text NOT NULL DEFAULT '',
  application_kind text NOT NULL DEFAULT '',
  resource_id text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT '',
  device_ids text[] NOT NULL DEFAULT '{}',
  version bigint NOT NULL CHECK (version > 0),
  created_at bigint NOT NULL DEFAULT 0,
  body jsonb NOT NULL,
  PRIMARY KEY (tenant_id,kind,id),
  CHECK (jsonb_typeof(body) = 'object')
);
CREATE INDEX IF NOT EXISTS analysis_document_queue ON analysis_document
  (tenant_id,application_kind,status,created_at,id) WHERE kind='run';
CREATE INDEX IF NOT EXISTS analysis_document_run_device ON analysis_document
  (tenant_id,run_id,kind,device_id,created_at,id);
CREATE UNIQUE INDEX IF NOT EXISTS analysis_document_resource_version ON analysis_document
  (tenant_id,application_kind,resource_id,(body->>'scope'),
   (CASE WHEN body->>'scope'='PERSONAL' THEN body->>'creator' ELSE '' END),
   ((body->>'version')::bigint)) WHERE kind='config';

CREATE INDEX IF NOT EXISTS analysis_document_ai_queue ON analysis_document
  (tenant_id,application_kind,status,created_at,id) WHERE kind='ai';
