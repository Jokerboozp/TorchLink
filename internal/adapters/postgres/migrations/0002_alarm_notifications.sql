-- Fire alarm notification channels, policies and the delivery queue.
CREATE TABLE IF NOT EXISTS notification_channel (
  tenant_id text NOT NULL, id text NOT NULL, body jsonb NOT NULL, secret text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1, updated_at bigint NOT NULL,
  PRIMARY KEY (tenant_id, id)
);
CREATE TABLE IF NOT EXISTS notification_policy (
  tenant_id text NOT NULL, id text NOT NULL, body jsonb NOT NULL,
  version bigint NOT NULL DEFAULT 1, updated_at bigint NOT NULL,
  PRIMARY KEY (tenant_id, id)
);
CREATE TABLE IF NOT EXISTS notification_task (
  id bigserial PRIMARY KEY,
  tenant_id text NOT NULL, alarm_id text NOT NULL, policy_id text NOT NULL, policy_name text NOT NULL DEFAULT '',
  stage integer NOT NULL, kind text NOT NULL, channel_id text NOT NULL,
  status text NOT NULL, attempts integer NOT NULL DEFAULT 0, next_at bigint NOT NULL,
  lease_until bigint NOT NULL DEFAULT 0, last_error text NOT NULL DEFAULT '',
  recipients jsonb NOT NULL DEFAULT '[]', created_at bigint NOT NULL, sent_at bigint NOT NULL DEFAULT 0,
  UNIQUE (tenant_id, alarm_id, policy_id, stage, kind, channel_id)
);
CREATE INDEX IF NOT EXISTS notification_task_due_idx ON notification_task(next_at) WHERE status = 'PENDING';
CREATE INDEX IF NOT EXISTS notification_task_lease_idx ON notification_task(lease_until) WHERE status = 'SENDING';
CREATE INDEX IF NOT EXISTS notification_task_alarm_idx ON notification_task(tenant_id, alarm_id);
CREATE INDEX IF NOT EXISTS notification_task_created_idx ON notification_task(created_at);
