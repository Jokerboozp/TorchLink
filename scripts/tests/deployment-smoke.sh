#!/usr/bin/env bash
# Real Compose config parsing; all Docker mutations and HTTP calls are mocked.
set -Eeuo pipefail
test_root="$(mktemp -d "${TMPDIR:-/tmp}/iot-deploy-test.XXXXXX")"
test_root="$(cd "$test_root" && pwd)"
trap 'rm -rf -- "$test_root"' EXIT
scripts="$(cd "$(dirname "$0")/.." && pwd)"
export TEST_COMPOSE="${1:?Pass the standalone docker-compose executable path}"
export TEST_CALLS="$test_root/calls.log" TEST_HTTP="$test_root/http.log"
export TEST_FAIL_BUILD=0 TEST_MISSING_IMAGE=0
: > "$TEST_CALLS"; : > "$TEST_HTTP"
while IFS= read -r key; do
  case "$key" in IOT_*|COMPOSE_*|POSTGRES_*|REDIS_*|CLICKHOUSE_*|MINIO_*|EMQX_*|GRAFANA_*|DEEPSEEK_*) unset "$key";; esac
done < <(compgen -e)

docker() {
  printf '%s\n' "$*" >> "$TEST_CALLS"
  if [ "$1" = compose ] && [[ " $* " == *' config '* ]]; then
    "$TEST_COMPOSE" "${@:2}"
  elif [ "$TEST_FAIL_BUILD" = 1 ] && [[ " $* " == *' build '* ]]; then
    return 42
  elif [ "$TEST_MISSING_IMAGE" = 1 ] && [ "$1" = image ]; then
    return 43
  elif [ "$1" = save ]; then
    printf 'mock images' > "$3"
  elif [ "$1" = run ] && [[ "${!#}" == 'tar -czf'* ]]; then
    local argument destination
    for argument in "$@"; do
      case "$argument" in type=bind,source=*,target=/backup)
        destination="${argument#type=bind,source=}"; destination="${destination%,target=/backup}"
        printf 'mock models' > "$destination/ollama-data.tgz";;
      esac
    done
  fi
}
curl() { printf '%s\n' "$*" >> "$TEST_HTTP"; if [[ " $* " == *' --write-out '* ]]; then printf 200; fi; return 0; }
go() { printf 'go %s\n' "$*" >> "$TEST_CALLS"; }
npm() { printf 'npm %s\n' "$*" >> "$TEST_CALLS"; }
export -f docker curl go npm
assert_call() { grep -Eq -- "$1" "$TEST_CALLS" || { printf 'Missing expected call: %s\n' "$1" >&2; exit 1; }; }
assert_no_call() { if grep -Eq -- "$1" "$TEST_CALLS"; then printf 'Unexpected call: %s\n' "$1" >&2; exit 1; fi; }

bash "$scripts/setup-local.sh" --env-file "$test_root/.env.local"
assert_call 'go mod download'
assert_call 'npm ci'
assert_call 'exec -T ollama ollama pull nomic-embed-text'
cp "$test_root/.env.local" "$test_root/local-original"
bash "$scripts/setup-local.sh" --env-file "$test_root/.env.local" --skip-code-deps
cmp "$test_root/local-original" "$test_root/.env.local"
echo 'PASS local: dependency preparation and unchanged configuration on rerun'

bash "$scripts/setup-local.sh" --env-file "$test_root/.env.remote" --skip-code-deps --dependency-host 192.168.24.133
grep -q "^IOT_LOCAL_BIND_ADDRESS='0.0.0.0'$" "$test_root/.env.remote"
grep -q "^IOT_LOCAL_ADVERTISED_HOST='192.168.24.133'$" "$test_root/.env.remote"
grep -q "^IOT_POSTGRES_DSN='postgres://.*@192.168.24.133:15432/iot?sslmode=disable'$" "$test_root/.env.remote"
grep -q "^IOT_KAFKA_BROKERS='192.168.24.133:19092'$" "$test_root/.env.remote"
remote_compose="$test_root/remote-compose.yaml"
"$TEST_COMPOSE" --project-name iot-platform-local --env-file "$test_root/.env.remote" -f "$scripts/../compose.local.yaml" config > "$remote_compose"
grep -q 'host_ip: 0.0.0.0' "$remote_compose"
grep -q 'external://192.168.24.133:19092' "$remote_compose"
grep -q 'image: postgres:17-alpine3.22' "$remote_compose"
grep -A80 '^  backup-service:' "$remote_compose" | grep -A2 'redpanda-init:' | grep -q 'condition: service_completed_successfully'
echo 'PASS local remote-host: published dependencies and advertised addresses'

bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online"
cp "$test_root/.env.online" "$test_root/online-original"
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online"
cmp "$test_root/online-original" "$test_root/.env.online"
assert_call 'build --pull platform-api platform-web backup-service'
grep -q '8081/health/ready' "$TEST_HTTP"
grep -q '8092/health/live' "$TEST_HTTP"
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online" --include-ai
grep -q '^IOT_AI_PROVIDER=ollama$' "$test_root/.env.online"
cmp <(grep '^IOT_ADMIN_PASSWORD=' "$test_root/online-original") <(grep '^IOT_ADMIN_PASSWORD=' "$test_root/.env.online")
TEST_FAIL_BUILD=1
: > "$TEST_CALLS"
if bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online"; then echo 'Build failure ignored' >&2; exit 1; fi
assert_no_call ' up '
TEST_FAIL_BUILD=0
echo 'PASS online: build, health checks, AI, repeatability and failure handling'

: > "$TEST_CALLS"
bash "$scripts/package-offline.sh" --output-dir "$test_root/bundles"
bundles=("$test_root"/bundles/iot-platform-offline-*)
bundle="${bundles[0]}"
[ -s "$bundle/ollama-data.tgz.sha256" ]
grep -q 'ollama/ollama:' "$bundle/manifest.json"
grep -q 'weaviate:' "$bundle/manifest.json"
assert_call 'exec -T ollama ollama pull nomic-embed-text'
cp "$bundle/.env.offline" "$test_root/offline-original"
: > "$TEST_CALLS"
bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"
bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"
cmp "$test_root/offline-original" "$bundle/.env.offline"
assert_call 'up -d --no-build --pull never'
assert_call '^run --rm --pull never'
assert_no_call ' build |ollama pull| compose .* pull '
TEST_MISSING_IMAGE=1
: > "$TEST_CALLS"
if bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"; then echo 'Missing image ignored' >&2; exit 1; fi
assert_no_call ' up '
TEST_MISSING_IMAGE=0
printf 'corruption' >> "$bundle/images.tar"
: > "$TEST_CALLS"
if bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"; then echo 'Corrupt archive accepted' >&2; exit 1; fi
assert_no_call '^load '
echo 'PASS offline: complete bundle, no network, repeatability and corrupt/missing image checks'
echo 'Bash deployment smoke tests PASS (mutations mocked; Compose parsing real).'
