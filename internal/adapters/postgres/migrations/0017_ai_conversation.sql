-- Assistant conversations per tenant, user and access version. Retention
-- removes a conversation by its last activity together with its messages.
CREATE TABLE IF NOT EXISTS ai_conversation (
  tenant_id text NOT NULL,
  actor text NOT NULL,
  id text NOT NULL,
  workflow_id text NOT NULL,
  title text NOT NULL DEFAULT '',
  access_version text NOT NULL DEFAULT '',
  message_count integer NOT NULL DEFAULT 0,
  created_at bigint NOT NULL,
  updated_at bigint NOT NULL,
  PRIMARY KEY (tenant_id, actor, id)
);
CREATE INDEX IF NOT EXISTS ai_conversation_list_idx ON ai_conversation (tenant_id, actor, workflow_id, access_version, updated_at DESC);
CREATE INDEX IF NOT EXISTS ai_conversation_updated_idx ON ai_conversation (updated_at);
CREATE TABLE IF NOT EXISTS ai_conversation_message (
  tenant_id text NOT NULL,
  actor text NOT NULL,
  conversation_id text NOT NULL,
  seq integer NOT NULL,
  role text NOT NULL,
  text text NOT NULL,
  run_id text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT '',
  created_at bigint NOT NULL,
  PRIMARY KEY (tenant_id, actor, conversation_id, seq),
  FOREIGN KEY (tenant_id, actor, conversation_id) REFERENCES ai_conversation (tenant_id, actor, id) ON DELETE CASCADE
);
