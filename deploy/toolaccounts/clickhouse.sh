#!/bin/sh
# Run once after ClickHouse is healthy. Existing users are never changed.
set +x
set -eu

fail() { printf '%s\n' "ClickHouse 工具账号初始化失败：$1" >&2; exit 1; }

: "${SERVICE_ADMIN_USER=admin}"
: "${SERVICE_ADMIN_PASSWORD=admin123}"
[ -n "$SERVICE_ADMIN_USER" ] || fail 'SERVICE_ADMIN_USER 不能为空。'
[ -n "$SERVICE_ADMIN_PASSWORD" ] || fail 'SERVICE_ADMIN_PASSWORD 不能为空。'
CLICKHOUSE_HOST=${CLICKHOUSE_HOST:-clickhouse}
CLICKHOUSE_PORT=${CLICKHOUSE_PORT:-9000}
CLICKHOUSE_USER=${CLICKHOUSE_USER:-iot}
export CLICKHOUSE_HOST CLICKHOUSE_USER
[ -n "${CLICKHOUSE_PASSWORD:-}" ] || fail '需要现有 ClickHouse 管理账号密码。'
export CLICKHOUSE_PASSWORD
for tool in clickhouse-client od awk sha256sum; do
  command -v "$tool" >/dev/null 2>&1 || fail "镜像中缺少 $tool。"
done

# Encode every username byte as a ClickHouse string-literal hex escape. This
# also preserves trailing newlines and Unicode without shell/SQL interpolation.
name_bytes=$(printf '%s' "$SERVICE_ADMIN_USER" | od -An -v -tx1) || fail '无法编码账号名称。'
name_sql=$(printf '%s\n' "$name_bytes" | awk '{ for (i=1; i<=NF; i++) printf "\\x%s", $i }') || fail '无法编码账号名称。'

run_query() {
  # ClickHouse 25.7 natively reads CLICKHOUSE_PASSWORD from the environment.
  # Keep queries on stdin and suppress diagnostics that may include credentials.
  clickhouse-client --host "$CLICKHOUSE_HOST" --port "$CLICKHOUSE_PORT" \
    --user "$CLICKHOUSE_USER" --connect_timeout 10 --receive_timeout 30 \
    --format TSVRaw 2>/dev/null
}

exists=$(printf "SELECT count() FROM system.users WHERE name = '%s';\n" "$name_sql" | run_query) || fail '无法查询已有账号，请检查连接凭据和管理权限。'
case "$exists" in
  1) printf '%s\n' 'ClickHouse 工具账号已存在，保持原密码和权限。'; exit 0 ;;
  0) ;;
  *) fail '账号查询返回了非预期结果。' ;;
esac

# Only a SHA-256 hash reaches SQL; plaintext passwords never enter query logs.
hash_output=$(printf '%s' "$SERVICE_ADMIN_PASSWORD" | sha256sum) || fail '无法计算账号密码摘要。'
password_hash=${hash_output%% *}
case "$password_hash" in *[!0-9a-fA-F]*|'') fail '密码摘要无效。' ;; esac
[ "${#password_hash}" -eq 64 ] || fail '密码摘要无效。'

# Do not use IF NOT EXISTS here: a concurrent creator must make this run fail
# before GRANT, so this script cannot change another creator's existing user.
if ! printf "CREATE USER '%s' IDENTIFIED WITH sha256_hash BY '%s';\n" "$name_sql" "$password_hash" | run_query >/dev/null; then
  fail '创建账号失败；已有账号未修改。'
fi
# Copy only privileges held by the bootstrap account. Official image users may
# lack NAMED COLLECTION ADMIN, so GRANT ALL would request unavailable rights.
if ! printf "GRANT CURRENT GRANTS ON *.* TO '%s' WITH GRANT OPTION;\n" "$name_sql" | run_query >/dev/null; then
  # This run successfully created the user. Remove only that new user so the
  # next one-shot attempt can retry instead of accepting a half-created account.
  if printf "DROP USER '%s';\n" "$name_sql" | run_query >/dev/null; then
    fail '授权失败，本次新建账号已撤销，可以排查权限后重试。'
  fi
  fail '授权失败，且无法撤销本次新建账号；请使用现有管理账号手工恢复。'
fi
printf '%s\n' 'ClickHouse 工具账号已创建并授权。'
