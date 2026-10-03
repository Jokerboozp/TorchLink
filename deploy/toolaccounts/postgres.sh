#!/bin/sh
# Run once after PostgreSQL is healthy. Existing roles are never changed.
set +x
set -eu

fail() { printf '%s\n' "PostgreSQL 工具账号初始化失败：$1" >&2; exit 1; }

: "${SERVICE_ADMIN_USER=admin}"
: "${SERVICE_ADMIN_PASSWORD=admin123}"
[ -n "$SERVICE_ADMIN_USER" ] || fail 'SERVICE_ADMIN_USER 不能为空。'
[ -n "$SERVICE_ADMIN_PASSWORD" ] || fail 'SERVICE_ADMIN_PASSWORD 不能为空。'
# PostgreSQL otherwise silently truncates identifiers to 63 bytes.
LC_ALL=C
export LC_ALL SERVICE_ADMIN_USER SERVICE_ADMIN_PASSWORD
[ "${#SERVICE_ADMIN_USER}" -le 63 ] || fail '账号名称不能超过 63 字节。'

PGHOST=${PGHOST:-postgres}
PGPORT=${PGPORT:-5432}
PGDATABASE=${PGDATABASE:-${POSTGRES_DB:-iot}}
PGUSER=${PGUSER:-${POSTGRES_USER:-iot}}
PGPASSWORD=${PGPASSWORD:-${POSTGRES_PASSWORD:-}}
PGCONNECT_TIMEOUT=${PGCONNECT_TIMEOUT:-10}
export PGHOST PGPORT PGDATABASE PGUSER PGPASSWORD PGCONNECT_TIMEOUT
[ -n "$PGPASSWORD" ] || fail '需要现有 PostgreSQL 管理账号密码。'
command -v psql >/dev/null 2>&1 || fail '镜像中缺少 psql。'

# psql reads secrets from the environment, not command arguments. Its native
# literal quoting and format(%I/%L) handle quotes, backslashes and newlines.
# Suppress client diagnostics because failed statements may contain passwords.
if ! psql -X --no-password --quiet --set=ON_ERROR_STOP=1 >/dev/null 2>&1 <<'SQL'
\getenv tool_admin_user SERVICE_ADMIN_USER
\getenv tool_admin_password SERVICE_ADMIN_PASSWORD
BEGIN;
SET LOCAL statement_timeout = '30s';
SELECT pg_advisory_xact_lock(1414286160, 1701079405);
SELECT format('CREATE ROLE %I LOGIN SUPERUSER PASSWORD %L', :'tool_admin_user', :'tool_admin_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'tool_admin_user')
\gexec
COMMIT;
SQL
then
  fail '连接或创建失败，请检查目标主库、连接凭据和管理权限；已有账号未修改。'
fi
printf '%s\n' 'PostgreSQL 工具账号初始化完成；已存在的账号保持不变。'
