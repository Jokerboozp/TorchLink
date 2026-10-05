-- Finished AI workflow runs: sizes, token usage, tool calls and outcome for
-- cost and quality review. Prompts and answers are not stored.
CREATE TABLE IF NOT EXISTS ai_workflow_run (
  run_id text PRIMARY KEY,
  tenant_id text NOT NULL,
  actor text NOT NULL DEFAULT '',
  workflow_id text NOT NULL,
  prompt_version text NOT NULL DEFAULT '',
  model text NOT NULL DEFAULT '',
  input_bytes integer NOT NULL DEFAULT 0,
  output_bytes integer NOT NULL DEFAULT 0,
  input_tokens bigint NOT NULL DEFAULT 0,
  output_tokens bigint NOT NULL DEFAULT 0,
  cache_read_tokens bigint NOT NULL DEFAULT 0,
  reasoning_tokens bigint NOT NULL DEFAULT 0,
  usage_reported boolean NOT NULL DEFAULT false,
  tool_calls integer NOT NULL DEFAULT 0,
  duration_ms bigint NOT NULL DEFAULT 0,
  status text NOT NULL,
  error text NOT NULL DEFAULT '',
  started_at bigint NOT NULL,
  finished_at bigint NOT NULL
);
CREATE INDEX IF NOT EXISTS ai_workflow_run_tenant_started_idx ON ai_workflow_run (tenant_id, started_at DESC);
CREATE INDEX IF NOT EXISTS ai_workflow_run_workflow_idx ON ai_workflow_run (tenant_id, workflow_id, started_at);
CREATE INDEX IF NOT EXISTS ai_workflow_run_started_idx ON ai_workflow_run (started_at);
