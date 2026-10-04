-- Desired dynamic Agent manifests. Every Harness instance is reconciled to
-- this table; deleted rows stay as tombstones so an instance that missed a
-- delete is cleaned up instead of spreading the Agent again.
CREATE TABLE IF NOT EXISTS ai_workflow_manifest (
  id text PRIMARY KEY,
  manifest jsonb NOT NULL,
  deleted boolean NOT NULL DEFAULT false,
  updated_at timestamptz NOT NULL DEFAULT now()
);
