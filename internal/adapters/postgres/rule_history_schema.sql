CREATE TABLE IF NOT EXISTS alarm_rule_revision (
 tenant_id text NOT NULL, id text NOT NULL, rule_id text NOT NULL, version integer NOT NULL,
 hash text NOT NULL, registered_at bigint NOT NULL, body jsonb NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,rule_id,version)
);
CREATE TABLE IF NOT EXISTS alarm_rule_activation (
 tenant_id text NOT NULL, rule_id text NOT NULL, revision_id text NOT NULL,
 version integer NOT NULL, since_at bigint NOT NULL, deleted boolean NOT NULL, body jsonb NOT NULL,
 PRIMARY KEY(tenant_id,rule_id,version)
);
CREATE TABLE IF NOT EXISTS alarm_rule_current_revision (
 tenant_id text NOT NULL, rule_id text NOT NULL, revision_id text NOT NULL,
 version integer NOT NULL, deleted boolean NOT NULL,
 PRIMARY KEY(tenant_id,rule_id)
);
CREATE TABLE IF NOT EXISTS alarm_rule_revision_pending (
 tenant_id text NOT NULL, rule_id text NOT NULL, revision_id text NOT NULL, device_id text NOT NULL,
 since_at bigint NOT NULL, PRIMARY KEY(tenant_id,revision_id,device_id)
);
CREATE INDEX IF NOT EXISTS alarm_rule_revision_pending_rule ON alarm_rule_revision_pending(tenant_id,rule_id);
CREATE TABLE IF NOT EXISTS rule_evaluation_trace (
 tenant_id text NOT NULL, id text NOT NULL, message_id text NOT NULL, claim_token bigint NOT NULL,
 device_id text NOT NULL, message_timestamp bigint NOT NULL, started_at bigint NOT NULL,
 status text NOT NULL, body jsonb NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,message_id,claim_token)
);
CREATE INDEX IF NOT EXISTS rule_evaluation_trace_scope ON rule_evaluation_trace(tenant_id,device_id,message_timestamp,started_at,id);
