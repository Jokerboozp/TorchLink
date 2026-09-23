#!/usr/bin/env bash
# Real Compose config parsing; all Docker mutations and HTTP calls are mocked.
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
test_root="$(mktemp -d "${TMPDIR:-/tmp}/iot-deploy-test.XXXXXX")"
# 执行当前脚本步骤。
test_root="$(cd "$test_root" && pwd)"
# 执行当前脚本步骤。
trap 'rm -rf -- "$test_root"' EXIT
# 执行当前脚本步骤。
scripts="$(cd "$(dirname "$0")/.." && pwd)"
# 执行当前脚本步骤。
bash "$scripts/tests/git-compat-smoke.sh"
# 执行当前脚本步骤。
bash "$scripts/tests/local-bootstrap-smoke.sh"
# 执行当前脚本步骤。
export TEST_COMPOSE="${1:?Pass the standalone docker-compose executable path}"
# 执行当前脚本步骤。
export TEST_CALLS="$test_root/calls.log" TEST_HTTP="$test_root/http.log"
# 执行当前脚本步骤。
export TEST_FAIL_BUILD=0 TEST_MISSING_IMAGE=0
# 执行当前脚本步骤。
: > "$TEST_CALLS"; : > "$TEST_HTTP"
# 遍历数据并执行循环体。
while IFS= read -r key; do
  # 执行当前脚本步骤。
  case "$key" in IOT_*|COMPOSE_*|POSTGRES_*|REDIS_*|CLICKHOUSE_*|MINIO_*|EMQX_*|GRAFANA_*|DEEPSEEK_*) unset "$key";; esac
# 结束当前控制块。
done < <(compgen -e)

# 执行当前脚本步骤。
docker() {
  # 执行当前脚本步骤。
  printf '%s\n' "$*" >> "$TEST_CALLS"
  # 判断条件后执行对应操作。
  if [ "$1" = info ] && [[ " $* " == *' --format '* ]]; then
    # 执行当前脚本步骤。
    printf x86_64
  # 执行当前脚本步骤。
  elif [ "$1" = compose ] && [ "${2:-}" = version ]; then
    # 执行当前脚本步骤。
    printf '2.27.3\n'
  # 执行当前脚本步骤。
  elif [ "$1" = compose ] && [[ " $* " == *' config '* ]]; then
    # 执行当前脚本步骤。
    "$TEST_COMPOSE" "${@:2}"
  # 执行当前脚本步骤。
  elif [ "$TEST_FAIL_BUILD" = 1 ] && [[ " $* " == *' build '* ]]; then
    # 返回结果或结束当前脚本。
    return 42
  # 执行当前脚本步骤。
  elif [ "$TEST_MISSING_IMAGE" = 1 ] && [ "$1" = image ]; then
    # 返回结果或结束当前脚本。
    return 43
  # 执行当前脚本步骤。
  elif [ "$1" = save ]; then
    # 执行当前脚本步骤。
    printf 'mock images' > "$3"
  # 执行当前脚本步骤。
  elif [ "$1" = run ] && [[ "${!#}" == 'tar -czf'* ]]; then
    # 执行当前脚本步骤。
    local argument destination
    # 遍历数据并执行循环体。
    for argument in "$@"; do
      # 执行当前脚本步骤。
      case "$argument" in type=bind,source=*,target=/backup)
        # 执行当前脚本步骤。
        destination="${argument#type=bind,source=}"; destination="${destination%,target=/backup}"
        # 执行当前脚本步骤。
        printf 'mock models' > "$destination/ollama-data.tgz";;
      # 执行当前脚本步骤。
      esac
    # 结束当前控制块。
    done
  # 结束当前控制块。
  fi
# 结束当前控制块。
}
# 执行当前脚本步骤。
curl() {
  # 执行当前脚本步骤。
  printf '%s\n' "$*" >> "$TEST_HTTP"
  # 判断条件后执行对应操作。
  if [[ " $* " == *' --write-out '* ]]; then printf 200; fi
  # 遍历数据并执行循环体。
  while [ "$#" -gt 0 ]; do
    # 判断条件后执行对应操作。
    if [ "$1" = --output ]; then printf 'mock runtime' > "$2"; break; fi
    # 执行当前脚本步骤。
    shift
  # 结束当前控制块。
  done
  # 返回结果或结束当前脚本。
  return 0
# 结束当前控制块。
}
# 执行当前脚本步骤。
go() { printf 'go %s\n' "$*" >> "$TEST_CALLS"; }
# 执行当前脚本步骤。
npm() { printf 'npm %s\n' "$*" >> "$TEST_CALLS"; }
# 执行当前脚本步骤。
export -f docker curl go npm
# 执行当前脚本步骤。
assert_call() { grep -Eq -- "$1" "$TEST_CALLS" || { printf 'Missing expected call: %s\n' "$1" >&2; exit 1; }; }
# 执行当前脚本步骤。
assert_no_call() { if grep -Eq -- "$1" "$TEST_CALLS"; then printf 'Unexpected call: %s\n' "$1" >&2; exit 1; fi; }
# 执行当前脚本步骤。
assert_commented_env() {
  # 执行当前脚本步骤。
  awk '
    /^[[:space:]]*(export[[:space:]]+)?[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=/ {
      if (previous !~ /^# 配置说明：/) exit 1
    }
    { previous=$0 }
  ' "$1" || { printf 'Configuration assignment is missing a Chinese comment: %s\n' "$1" >&2; exit 1; }
# 结束当前控制块。
}

# 执行当前脚本步骤。
bash "$scripts/setup-local.sh" --env-file "$test_root/.env.local"
# 执行当前脚本步骤。
grep -q "^IOT_AI_PROVIDER='deepseek'$" "$test_root/.env.local"
# 执行当前脚本步骤。
grep -q "^IOT_AI_BASE_URL='https://api.deepseek.com'$" "$test_root/.env.local"
# 执行当前脚本步骤。
grep -q "^IOT_AI_MODEL='deepseek-v4-flash'$" "$test_root/.env.local"
# 执行当前脚本步骤。
grep -q "^IOT_AI_HARNESS_ENABLED='true'$" "$test_root/.env.local"
# 执行当前脚本步骤。
grep -q "^IOT_AI_HARNESS_URL='http://127.0.0.1:8091'$" "$test_root/.env.local"
# 执行当前脚本步骤。
grep -q "^IOT_BACKUP_URL='http://127.0.0.1:8092'$" "$test_root/.env.local"
# 执行当前脚本步骤。
assert_commented_env "$test_root/.env.local"
# 执行当前脚本步骤。
assert_call '--profile harness up -d --build --wait'
# 判断条件后执行对应操作。
if grep -Eq ' up .*backup-service' "$TEST_CALLS"; then echo 'Local setup unexpectedly started backup-service' >&2; exit 1; fi
# 执行当前脚本步骤。
assert_call 'go mod download'
# 执行当前脚本步骤。
assert_call 'npm ci'
# 执行当前脚本步骤。
assert_call 'exec -T ollama ollama pull nomic-embed-text'
# 执行当前脚本步骤。
cp "$test_root/.env.local" "$test_root/local-original"
# 执行当前脚本步骤。
bash "$scripts/setup-local.sh" --env-file "$test_root/.env.local" --skip-code-deps
# 执行当前脚本步骤。
cmp "$test_root/local-original" "$test_root/.env.local"
# 执行当前脚本步骤。
echo 'PASS local: dependency preparation and unchanged configuration on rerun'

# 执行当前脚本步骤。
no_harness_env="$test_root/.env.no-harness"
# 执行当前脚本步骤。
cp "$test_root/.env.local" "$no_harness_env"
# 执行当前脚本步骤。
sed "s/^IOT_AI_HARNESS_ENABLED=.*/IOT_AI_HARNESS_ENABLED='false'/" "$no_harness_env" > "$test_root/no-harness.tmp"
# 执行当前脚本步骤。
mv "$test_root/no-harness.tmp" "$no_harness_env"
# 执行当前脚本步骤。
: > "$TEST_CALLS"
# 执行当前脚本步骤。
bash "$scripts/setup-local.sh" --env-file "$no_harness_env" --skip-code-deps
# 执行当前脚本步骤。
grep -q "^IOT_AI_HARNESS_URL=''$" "$no_harness_env"
# 执行当前脚本步骤。
assert_no_call '--profile harness'
# 执行当前脚本步骤。
echo 'PASS local configuration: Harness can be disabled in the environment file'

# 执行当前脚本步骤。
bash "$scripts/setup-local.sh" --env-file "$test_root/.env.remote" --skip-code-deps --dependency-host 192.168.24.133 --api-host 192.168.24.1
# 执行当前脚本步骤。
grep -q "^IOT_LOCAL_BIND_ADDRESS='0.0.0.0'$" "$test_root/.env.remote"
# 执行当前脚本步骤。
grep -q "^IOT_LOCAL_ADVERTISED_HOST='192.168.24.133'$" "$test_root/.env.remote"
# 执行当前脚本步骤。
grep -q "^IOT_POSTGRES_DSN='postgres://.*@192.168.24.133:15432/iot?sslmode=disable'$" "$test_root/.env.remote"
# 执行当前脚本步骤。
grep -q "^IOT_KAFKA_BROKERS='192.168.24.133:19092'$" "$test_root/.env.remote"
# 执行当前脚本步骤。
grep -q "^IOT_AI_HARNESS_MCP_URL='http://192.168.24.1:8081/mcp/harness'$" "$test_root/.env.remote"
# 执行当前脚本步骤。
grep -q "^IOT_HARNESS_MCP_ALLOWED_ORIGINS='http://192.168.24.1:8081'$" "$test_root/.env.remote"
# 执行当前脚本步骤。
remote_compose="$test_root/remote-compose.yaml"
# 执行当前脚本步骤。
"$TEST_COMPOSE" --project-name iot-platform-local --env-file "$test_root/.env.remote" -f "$scripts/../compose.local.yaml" --profile harness config > "$remote_compose"
# 执行当前脚本步骤。
grep -q 'host_ip: 0.0.0.0' "$remote_compose"
# 执行当前脚本步骤。
grep -q 'external://192.168.24.133:19092' "$remote_compose"
# 执行当前脚本步骤。
grep -q 'image: postgres:17-alpine3.22' "$remote_compose"
# 执行当前脚本步骤。
grep -q 'IOT_HARNESS_MCP_ALLOWED_ORIGINS: http://192.168.24.1:8081' "$remote_compose"
# 判断条件后执行对应操作。
if grep -q '^  backup-service:' "$remote_compose"; then echo 'Local default Compose unexpectedly includes backup-service' >&2; exit 1; fi
# 执行当前脚本步骤。
backup_compose="$test_root/remote-backup-compose.yaml"
# 执行当前脚本步骤。
"$TEST_COMPOSE" --project-name iot-platform-local --env-file "$test_root/.env.remote" -f "$scripts/../compose.local.yaml" --profile backup config > "$backup_compose"
# 执行当前脚本步骤。
grep -A80 '^  backup-service:' "$backup_compose" | grep -q 'IOT_CLICKHOUSE_URL'
# 执行当前脚本步骤。
echo 'PASS local remote-host: published dependencies and advertised addresses'

# 执行当前脚本步骤。
deepseek_env="$test_root/.env.deepseek"
# 执行当前脚本步骤。
cp "$test_root/.env.remote" "$deepseek_env"
# 执行当前脚本步骤。
printf "IOT_AI_API_KEY='smoke-test-key'\n" >> "$deepseek_env"
# 执行当前脚本步骤。
bash "$scripts/setup-local.sh" --env-file "$deepseek_env" --skip-code-deps --dependency-host 192.168.24.133 --api-host 192.168.24.1 --include-deepseek
# 执行当前脚本步骤。
grep -q "^IOT_AI_PROVIDER='deepseek'$" "$deepseek_env"
# 执行当前脚本步骤。
grep -q "^IOT_AI_BASE_URL='https://api.deepseek.com'$" "$deepseek_env"
# 执行当前脚本步骤。
grep -q "^IOT_AI_MODEL='deepseek-v4-flash'$" "$deepseek_env"
# 执行当前脚本步骤。
grep -q "^DEEPSEEK_API_KEY='smoke-test-key'$" "$deepseek_env"
# 判断条件后执行对应操作。
if tail -n 12 "$TEST_CALLS" | grep -q 'ollama pull qwen3:1.7b'; then echo 'DeepSeek setup attempted an Ollama chat model download' >&2; exit 1; fi
# 执行当前脚本步骤。
echo 'PASS local deepseek: provider enabled without local chat model download'

# 执行当前脚本步骤。
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online"
# 执行当前脚本步骤。
grep -q '^IOT_AI_PROVIDER=ollama$' "$test_root/.env.online"
# 执行当前脚本步骤。
grep -q '^IOT_AI_BASE_URL=http://ollama:11434$' "$test_root/.env.online"
# 执行当前脚本步骤。
grep -q '^IOT_AI_MODEL=qwen3:1.7b$' "$test_root/.env.online"
# 执行当前脚本步骤。
grep -q '^IOT_AI_HARNESS_ENABLED=true$' "$test_root/.env.online"
# 执行当前脚本步骤。
grep -q '^IOT_AI_HARNESS_URL=http://deepseek-harness:8091$' "$test_root/.env.online"
# 执行当前脚本步骤。
grep -q '^IOT_AI_HARNESS_PROVIDER=ollama$' "$test_root/.env.online"
# 执行当前脚本步骤。
grep -q '^IOT_AI_HARNESS_MODEL=qwen3:1.7b$' "$test_root/.env.online"
# 执行当前脚本步骤。
grep -q '^IOT_AI_HARNESS_OLLAMA_BASE_URL=http://ollama:11434/v1$' "$test_root/.env.online"
# 执行当前脚本步骤。
assert_commented_env "$test_root/.env.online"
# 执行当前脚本步骤。
grep -q '^IOT_ADMIN_PASSWORD=admin123$' "$test_root/.env.online"
# 执行当前脚本步骤。
assert_call 'build --pull platform-api platform-web backup-service deepseek-harness'
# 执行当前脚本步骤。
assert_call 'exec -T ollama ollama pull qwen3:1.7b'
# 执行当前脚本步骤。
cp "$test_root/.env.online" "$test_root/online-original"
# 执行当前脚本步骤。
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online"
# 执行当前脚本步骤。
cmp "$test_root/online-original" "$test_root/.env.online"
# 执行当前脚本步骤。
assert_call 'build --pull platform-api platform-web backup-service'
# 执行当前脚本步骤。
grep -q '8081/health/ready' "$TEST_HTTP"
# 执行当前脚本步骤。
grep -q '8092/health/ready' "$TEST_HTTP"
# 执行当前脚本步骤。
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online" --include-ai
# 执行当前脚本步骤。
grep -q '^IOT_AI_PROVIDER=ollama$' "$test_root/.env.online"
# 执行当前脚本步骤。
cmp <(grep '^IOT_ADMIN_PASSWORD=' "$test_root/online-original") <(grep '^IOT_ADMIN_PASSWORD=' "$test_root/.env.online")
# 执行当前脚本步骤。
TEST_FAIL_BUILD=1
# 执行当前脚本步骤。
: > "$TEST_CALLS"
# 判断条件后执行对应操作。
if bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online"; then echo 'Build failure ignored' >&2; exit 1; fi
# 执行当前脚本步骤。
assert_no_call ' up '
# 执行当前脚本步骤。
TEST_FAIL_BUILD=0
# 执行当前脚本步骤。
echo 'PASS online: build, health checks, AI, repeatability and failure handling'

# 执行当前脚本步骤。
: > "$TEST_CALLS"
# 执行当前脚本步骤。
bash "$scripts/package-offline.sh" --output-dir "$test_root/bundles"
# 执行当前脚本步骤。
bundles=("$test_root"/bundles/iot-platform-offline-*)
# 执行当前脚本步骤。
bundle="${bundles[0]}"
# 执行当前脚本步骤。
assert_commented_env "$bundle/.env.offline"
# 执行当前脚本步骤。
grep -q '^IOT_ADMIN_PASSWORD=admin123$' "$bundle/.env.offline"
# 执行当前脚本步骤。
[ -s "$bundle/ollama-data.tgz.sha256" ]
# 执行当前脚本步骤。
[ -s "$bundle/docker-runtime/docker-24.0.9.tgz.sha256" ]
# 执行当前脚本步骤。
[ -s "$bundle/docker-runtime/docker-28.5.2.tgz.sha256" ]
# 执行当前脚本步骤。
[ -s "$bundle/docker-runtime/docker-compose.sha256" ]
# 执行当前脚本步骤。
[ -f "$bundle/scripts/lib/docker-bootstrap.sh" ]
# 执行当前脚本步骤。
[ -f "$bundle/scripts/lib/restore-ollama-models.sh" ]
# 执行当前脚本步骤。
grep -q 'ollama/ollama:' "$bundle/manifest.json"
# 执行当前脚本步骤。
grep -q 'weaviate:' "$bundle/manifest.json"
# 执行当前脚本步骤。
assert_call 'exec -T ollama ollama pull nomic-embed-text'
# 执行当前脚本步骤。
assert_call 'exec -T ollama ollama pull qwen3:1.7b'
# 执行当前脚本步骤。
grep -q '^IOT_AI_PROVIDER=ollama$' "$bundle/.env.offline"
# 执行当前脚本步骤。
grep -q '^IOT_AI_MODEL=qwen3:1.7b$' "$bundle/.env.offline"
# 执行当前脚本步骤。
grep -q '^IOT_AI_HARNESS_PROVIDER=ollama$' "$bundle/.env.offline"
# 执行当前脚本步骤。
grep -qx 'harness' "$bundle/profiles.txt"
# 执行当前脚本步骤。
cp "$bundle/.env.offline" "$test_root/offline-original"
# 执行当前脚本步骤。
: > "$TEST_CALLS"
# 执行当前脚本步骤。
bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"
# 执行当前脚本步骤。
bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"
# 执行当前脚本步骤。
cmp "$test_root/offline-original" "$bundle/.env.offline"
# 执行当前脚本步骤。
assert_call 'up -d --no-build --pull never'
# 执行当前脚本步骤。
assert_call '^run --rm --pull never'
# 执行当前脚本步骤。
assert_no_call ' build |ollama pull| compose .* pull '
# 执行当前脚本步骤。
TEST_MISSING_IMAGE=1
# 执行当前脚本步骤。
: > "$TEST_CALLS"
# 判断条件后执行对应操作。
if bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle" > "$test_root/missing-image.log" 2>&1; then echo 'Missing image ignored' >&2; exit 1; fi
# 执行当前脚本步骤。
grep -q '离线包缺少镜像：' "$test_root/missing-image.log"
# 判断条件后执行对应操作。
if grep -q 'unbound variable' "$test_root/missing-image.log"; then echo 'Missing image diagnostic failed'; exit 1; fi
# 执行当前脚本步骤。
assert_no_call ' up '
# 执行当前脚本步骤。
TEST_MISSING_IMAGE=0
# 执行当前脚本步骤。
printf 'corruption' >> "$bundle/images.tar"
# 执行当前脚本步骤。
: > "$TEST_CALLS"
# 判断条件后执行对应操作。
if bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"; then echo 'Corrupt archive accepted' >&2; exit 1; fi
# 执行当前脚本步骤。
assert_no_call '^load '
# 执行当前脚本步骤。
echo 'PASS offline: complete bundle, no network, repeatability and corrupt/missing image checks'
# 执行当前脚本步骤。
echo 'Bash deployment smoke tests PASS (mutations mocked; Compose parsing real).'
