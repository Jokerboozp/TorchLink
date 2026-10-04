-- Relational access control: one row per user, role and API key, device
-- grants per subject, and one revision per tenant checked on every save.
CREATE TABLE IF NOT EXISTS platform_access_revision (
  tenant_id text PRIMARY KEY, revision bigint NOT NULL
);
CREATE TABLE IF NOT EXISTS platform_user (
  tenant_id text NOT NULL, username text NOT NULL, position integer NOT NULL DEFAULT 0, body jsonb NOT NULL,
  PRIMARY KEY (tenant_id, username)
);
CREATE TABLE IF NOT EXISTS platform_role (
  tenant_id text NOT NULL, id text NOT NULL, position integer NOT NULL DEFAULT 0, body jsonb NOT NULL,
  PRIMARY KEY (tenant_id, id)
);
CREATE TABLE IF NOT EXISTS platform_api_key (
  tenant_id text NOT NULL, id text NOT NULL, position integer NOT NULL DEFAULT 0, body jsonb NOT NULL,
  PRIMARY KEY (tenant_id, id)
);
CREATE TABLE IF NOT EXISTS access_device_grant (
  tenant_id text NOT NULL, subject_kind text NOT NULL CHECK (subject_kind IN ('user','role')),
  subject_id text NOT NULL, device_id text NOT NULL, position integer NOT NULL DEFAULT 0,
  PRIMARY KEY (tenant_id, subject_kind, subject_id, device_id)
);
CREATE INDEX IF NOT EXISTS access_device_grant_device_idx ON access_device_grant(tenant_id, device_id);
