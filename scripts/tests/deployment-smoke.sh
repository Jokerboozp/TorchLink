#!/usr/bin/env bash
# Real Compose config parsing; all Docker mutations and HTTP calls are mocked.
set -Eeuo pipefail
test_root="$(mktemp -d "${TMPDIR:-/tmp}/iot-deploy-test.XXXXXX")"
test_root="$(cd "$test_root" && pwd)"
trap 'rm -rf -- "$test_root"' EXIT
scripts="$(cd "$(dirname "$0")/.." && pwd)"
# git-compat
(
# Verifies deployment scripts against the Git version shipped by CentOS 7,
# which supports `git -c` but not the later global `git -C` option.
set -Eeuo pipefail

test_root="$(mktemp -d "${TMPDIR:-/tmp}/iot-git-compat-test.XXXXXX")"
trap 'rm -rf -- "$test_root"' EXIT
project_root="$(cd "$(dirname "$0")/../.." && pwd)"
fixture="$test_root/project"
expected_revision="$(tr -d '\r\n' < "$project_root/deploy/deepseek-harness/REVISION")"

mkdir -p \
  "$fixture/scripts" \
  "$fixture/deploy/deepseek-harness" \
  "$fixture/upstream/deepseek-harness/.git" \
  "$test_root/bin"
cp "$project_root/scripts/fetch-deepseek-harness.sh" "$fixture/scripts/"
cp "$project_root/deploy/deepseek-harness/REVISION" "$fixture/deploy/deepseek-harness/"

cat > "$test_root/bin/git" <<'EOF'
#!/usr/bin/env sh
if [ "${1:-}" = "-C" ]; then
  echo "Unknown option: -C" >&2
  exit 129
fi
case "${1:-} ${2:-}" in
  "status --porcelain")
    if [ -f .fresh-clone-dirty ]; then
      printf ' M package.json\n'
      exit 0
    fi
    if [ -n "${FAKE_GIT_DIRTY_FLAG:-}" ] && [ -f "$FAKE_GIT_DIRTY_FLAG" ]; then
      rm -f "$FAKE_GIT_DIRTY_FLAG"
      printf ' M package.json\n'
    fi
    exit 0 ;;
  "rev-parse HEAD") printf '%s\n' "$EXPECTED_REVISION" ;;
  "config core.autocrlf"|"config core.fileMode"|"clean -fd") exit 0 ;;
  "reset --hard") rm -f .fresh-clone-dirty; exit 0 ;;
  "-c http.version=HTTP/1.1")
    destination=''
    for argument in "$@"; do destination="$argument"; done
    mkdir -p "$destination/.git"
    touch "$destination/.fresh-clone-dirty"
    exit 0 ;;
  *) echo "Unexpected git invocation: $*" >&2; exit 2 ;;
esac
EOF
chmod +x "$test_root/bin/git"

PATH="$test_root/bin:$PATH" EXPECTED_REVISION="$expected_revision" \
  sh "$fixture/scripts/fetch-deepseek-harness.sh" > "$test_root/output.log"

grep -qx "$expected_revision" "$fixture/upstream/deepseek-harness.revision"
grep -q "DeepSeek Harness ready: $expected_revision" "$test_root/output.log"

# Re-running setup must not rewrite an unchanged Docker COPY input.
touch -t 200001010000 "$fixture/upstream/deepseek-harness.revision"
touch -t 200001010001 "$test_root/marker-cutoff"
PATH="$test_root/bin:$PATH" EXPECTED_REVISION="$expected_revision" \
  sh "$fixture/scripts/fetch-deepseek-harness.sh" > "$test_root/rerun-output.log"
if [ "$fixture/upstream/deepseek-harness.revision" -nt "$test_root/marker-cutoff" ]; then
  echo 'Unchanged Harness revision marker was rewritten' >&2
  exit 1
fi

touch "$test_root/dirty.flag"
PATH="$test_root/bin:$PATH" EXPECTED_REVISION="$expected_revision" FAKE_GIT_DIRTY_FLAG="$test_root/dirty.flag" \
  sh "$fixture/scripts/fetch-deepseek-harness.sh" > "$test_root/dirty-output.log"
backup_count="$(find "$fixture/upstream" -maxdepth 1 -type d -name 'deepseek-harness.backup-*' | wc -l | tr -d ' ')"
[ "$backup_count" = 1 ]
[ -d "$fixture/upstream/deepseek-harness/.git" ]
[ ! -e "$fixture/upstream/deepseek-harness/.fresh-clone-dirty" ]
grep -q '源码目录存在修改，已备份到：' "$test_root/dirty-output.log"

if grep -En 'git[[:space:]]+-C' \
  "$project_root/scripts/fetch-deepseek-harness.sh" \
  "$project_root/scripts/package-offline.sh"; then
  echo "部署脚本仍使用 CentOS 7 Git 不支持的全局 -C 参数" >&2
  exit 1
fi

echo 'PASS git compatibility: deployment scripts work without global git -C'
)
# Real Git fixture: Git before 2.10 treated `text=auto eol=lf` as
# `text eol=lf`, including binary files. Use that equivalent attribute to
# exercise the same conversion failure with modern Git, without the network.
(
set -Eeuo pipefail
project_root="$(cd "$(dirname "$0")/../.." && pwd)"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/iot-harness-git-test.XXXXXX")"
trap 'rm -rf -- "$test_root"' EXIT
fixture="$test_root/project"
origin="$test_root/origin"
mkdir -p "$fixture/scripts" "$fixture/deploy/deepseek-harness" "$origin"
cp "$project_root/scripts/fetch-deepseek-harness.sh" "$fixture/scripts/"
export GIT_CONFIG_GLOBAL="$test_root/gitconfig" GIT_CONFIG_NOSYSTEM=1
git init -q "$origin"
(
  cd "$origin"
  git config user.name 'Harness regression test'
  git config user.email 'test@example.invalid'
  git config core.autocrlf false
  # Store original bytes, then simulate the legacy client's read attributes.
  printf '* -text -eol\n' > .git/info/attributes
  printf '* text eol=lf\n' > .gitattributes
  printf '\211PNG\r\n\000pinned\r\n' > icon.png
  printf 'pinned\n' > source.txt
  git add .
  git commit -qm pinned
  git rev-parse HEAD > "$fixture/deploy/deepseek-harness/REVISION"
  printf '\211PNG\r\n\000latest\r\n' > icon.png
  printf 'latest\n' > source.txt
  git add .
  git commit -qm latest
)
origin_path="$origin"
if command -v cygpath >/dev/null 2>&1; then origin_path="$(cygpath -m "$origin")"; fi
git config --global "url.${origin_path}.insteadOf" 'https://github.com/deepseek-ai/deepseek-harness.git'
sh "$fixture/scripts/fetch-deepseek-harness.sh" > "$test_root/output.log" 2>&1 || {
  cat "$test_root/output.log" >&2
  echo 'FAIL: freshly cloned Harness binaries prevented pinned checkout' >&2
  exit 1
}
target="$fixture/upstream/deepseek-harness"
expected="$(cat "$fixture/deploy/deepseek-harness/REVISION")"
(
  cd "$target"
  [ "$(git rev-parse HEAD)" = "$expected" ]
  [ -z "$(git status --porcelain)" ]
  git show HEAD:icon.png > "$test_root/expected.png"
  cmp icon.png "$test_root/expected.png"
)
# Genuine user changes must still be preserved before rebuilding the source.
printf 'user edit\n' >> "$target/source.txt"
printf 'user file\n' > "$target/user.txt"
sh "$fixture/scripts/fetch-deepseek-harness.sh" > "$test_root/dirty.log" 2>&1
backup_count=0
for backup in "$fixture/upstream/"deepseek-harness.backup-*; do
  [ -d "$backup" ] || continue
  backup_count=$((backup_count + 1))
  grep -q 'user edit' "$backup/source.txt"
  grep -qx 'user file' "$backup/user.txt"
done
[ "$backup_count" = 1 ]
sh "$fixture/scripts/fetch-deepseek-harness.sh" > "$test_root/rerun.log" 2>&1
! grep -q '已备份' "$test_root/rerun.log"
echo 'PASS Harness real Git: binary bytes, pinned checkout, user backup and rerun'
# Reuse the same origin to verify the PowerShell entry point when available.
ps_runner=''
if command -v pwsh >/dev/null 2>&1; then ps_runner=pwsh
elif command -v powershell.exe >/dev/null 2>&1; then ps_runner=powershell.exe
fi
if [ -n "$ps_runner" ]; then
  ps_fixture="$test_root/powershell-project"
  mkdir -p "$ps_fixture/deploy/deepseek-harness"
  cp "$fixture/deploy/deepseek-harness/REVISION" "$ps_fixture/deploy/deepseek-harness/"
  export HARNESS_TEST_PROJECT="$ps_fixture" HARNESS_TEST_LIB="$project_root/scripts/lib/deployment.ps1"
  if command -v cygpath >/dev/null 2>&1; then
    HARNESS_TEST_PROJECT="$(cygpath -m "$HARNESS_TEST_PROJECT")"
    HARNESS_TEST_LIB="$(cygpath -m "$HARNESS_TEST_LIB")"
  fi
  cat > "$test_root/test-harness.ps1" <<'EOF'
$ErrorActionPreference = 'Stop'
. $env:HARNESS_TEST_LIB
Ensure-HarnessSource -ProjectRoot $env:HARNESS_TEST_PROJECT
EOF
  ps_test="$test_root/test-harness.ps1"
  if command -v cygpath >/dev/null 2>&1; then ps_test="$(cygpath -m "$ps_test")"; fi
  "$ps_runner" -NoProfile -File "$ps_test" > "$test_root/powershell.log" 2>&1 || { cat "$test_root/powershell.log" >&2; exit 1; }
  ps_target="$ps_fixture/upstream/deepseek-harness"
  (
    cd "$ps_target"
    [ "$(git rev-parse HEAD)" = "$expected" ]
    [ -z "$(git status --porcelain)" ]
    cmp icon.png "$test_root/expected.png"
  )
  printf 'user edit\n' >> "$ps_target/source.txt"
  printf 'user file\n' > "$ps_target/user.txt"
  "$ps_runner" -NoProfile -File "$ps_test" > "$test_root/powershell-dirty.log" 2>&1 || { cat "$test_root/powershell-dirty.log" >&2; exit 1; }
  backup_count=0
  for backup in "$ps_fixture/upstream/"deepseek-harness.backup-*; do
    [ -d "$backup" ] || continue
    backup_count=$((backup_count + 1))
    grep -q 'user edit' "$backup/source.txt"
    grep -qx 'user file' "$backup/user.txt"
  done
  [ "$backup_count" = 1 ]
  "$ps_runner" -NoProfile -File "$ps_test" > "$test_root/powershell-rerun.log" 2>&1
  ! grep -q 'backup-' "$test_root/powershell-rerun.log"
  echo 'PASS Harness PowerShell real Git: binary bytes, pinned checkout, user backup and rerun'
fi
)
if [ "${1:-}" = --harness-git-only ]; then exit 0; fi
# local-bootstrap
(
# Exercise setup-local's bootstrap ordering without installing host software.
set -Eeuo pipefail
project_root="$(cd "$(dirname "$0")/../.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/scripts/lib"
cp "$project_root/scripts/setup-local.sh" "$test_root/scripts/"
cp "$project_root/scripts/lib/"*.sh "$test_root/scripts/lib/"
export LOCAL_BOOTSTRAP_MARKER="$test_root/bootstrapped"
cat >> "$test_root/scripts/lib/docker-bootstrap.sh" <<'EOF'
ensure_deployment_docker() {
  [ "$1" = online ] || exit 93
  touch "$LOCAL_BOOTSTRAP_MARKER"
}
EOF
cat >> "$test_root/scripts/lib/deployment.sh" <<'EOF'
assert_docker_available() {
  [ -f "$LOCAL_BOOTSTRAP_MARKER" ] || { echo 'FAIL: local setup did not bootstrap missing Docker' >&2; exit 94; }
  # Stop before generating credentials or starting containers.
  exit 0
}
EOF
bash "$test_root/scripts/setup-local.sh" --skip-code-deps
echo 'PASS local setup bootstraps Docker before checking availability'
)
export TEST_COMPOSE="${1:?Pass the standalone docker-compose executable path}"
export TEST_CALLS="$test_root/calls.log" TEST_HTTP="$test_root/http.log"
export TEST_FAIL_BUILD=0 TEST_MISSING_IMAGE=0
: > "$TEST_CALLS"; : > "$TEST_HTTP"
while IFS= read -r key; do
  case "$key" in IOT_*|COMPOSE_*|POSTGRES_*|REDIS_*|CLICKHOUSE_*|MINIO_*|EMQX_*|GRAFANA_*|DEEPSEEK_*) unset "$key";; esac
done < <(compgen -e)

# Adding management observation must preserve existing credentials, and must
# reject incomplete pairs before making a previously working environment worse.
(
  source "$scripts/lib/deployment.sh"
  observation_env="$test_root/.env.observation"
  : > "$observation_env"
  ensure_emqx_admin_env "$observation_env" 'http://emqx:18083'
  test -n "$(get_deployment_env_value "$observation_env" IOT_EMQX_API_KEY)"
  test -n "$(get_deployment_env_value "$observation_env" IOT_EMQX_API_SECRET)"
  cp "$observation_env" "$observation_env.before"
  ensure_emqx_admin_env "$observation_env" 'http://different-host:18083'
  cmp "$observation_env.before" "$observation_env"
  printf 'IOT_EMQX_API_KEY=existing-test-key\n' > "$observation_env"
  cp "$observation_env" "$observation_env.before"
  if ensure_emqx_admin_env "$observation_env" 'http://emqx:18083' >/dev/null 2>&1; then
    echo 'Accepted an incomplete EMQX credential pair' >&2; exit 1
  fi
  cmp "$observation_env.before" "$observation_env"
)
echo 'PASS EMQX management credentials: generate once, preserve and reject incomplete pairs'

docker() {
  printf '%s\n' "$*" >> "$TEST_CALLS"
  if [ "$1" = info ] && [[ " $* " == *' --format '* ]]; then
    printf x86_64
  elif [ "$1" = compose ] && [ "${2:-}" = version ]; then
    printf '2.27.3\n'
  elif [ "$1" = compose ] && [[ " $* " == *' config '* ]]; then
    "$TEST_COMPOSE" "${@:2}"
  elif [ "$TEST_FAIL_BUILD" = 1 ] && [[ " $* " == *' build '* ]]; then
    return 42
  elif [ "$TEST_MISSING_IMAGE" = 1 ] && [ "$1" = image ]; then
    return 43
  elif [ "$1" = save ]; then
    printf 'mock images' > "$3"
  elif [ "$1" = run ] && [[ "$*" == *'/helpers/export-model-cache.sh'* ]]; then
    local argument destination archive
    for argument in "$@"; do
      case "$argument" in
        type=bind,source=*,target=/backup)
          destination="${argument#type=bind,source=}"; destination="${destination%,target=/backup}";;
        /backup/*) archive="${argument#/backup/}";;
      esac
    done
    printf 'mock models' > "$destination/$archive"
  fi
}
curl() {
  printf '%s\n' "$*" >> "$TEST_HTTP"
  if [[ " $* " == *' --write-out '* ]]; then printf 200; fi
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --output ]; then printf 'mock runtime' > "$2"; break; fi
    shift
  done
  return 0
}
go() { printf 'go %s\n' "$*" >> "$TEST_CALLS"; }
npm() { printf 'npm %s\n' "$*" >> "$TEST_CALLS"; }
export -f docker curl go npm
assert_call() { grep -Eq -- "$1" "$TEST_CALLS" || { printf 'Missing expected call: %s\n' "$1" >&2; exit 1; }; }
assert_no_call() { if grep -Eq -- "$1" "$TEST_CALLS"; then printf 'Unexpected call: %s\n' "$1" >&2; exit 1; fi; }
assert_commented_env() {
  awk '
    /^[[:space:]]*(export[[:space:]]+)?[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=/ {
      if (previous !~ /^# 配置说明：/) exit 1
    }
    { previous=$0 }
  ' "$1" || { printf 'Configuration assignment is missing a Chinese comment: %s\n' "$1" >&2; exit 1; }
}

bash "$scripts/setup-local.sh" --env-file "$test_root/.env.local"
grep -q "^IOT_AI_PROVIDER=deepseek$" "$test_root/.env.local"
grep -q "^IOT_AI_BASE_URL=https://api.deepseek.com$" "$test_root/.env.local"
grep -q "^IOT_AI_MODEL=deepseek-flash$" "$test_root/.env.local"
grep -q "^IOT_AI_HARNESS_ENABLED='true'$" "$test_root/.env.local"
grep -q "^IOT_AI_HARNESS_URL='http://127.0.0.1:8091'$" "$test_root/.env.local"
grep -q "^IOT_BACKUP_URL='http://127.0.0.1:8092'$" "$test_root/.env.local"
grep -q "^IOT_OPS_CAPACITY_LOCAL='true'$" "$test_root/.env.local"
grep -q "^IOT_CAPACITY_MODULE='on'$" "$test_root/.env.local"
assert_commented_env "$test_root/.env.local"
assert_call 'compose.local.yaml up -d --build --wait'
assert_no_call '--profile harness'
if grep -Eq ' up .*backup-service' "$TEST_CALLS"; then echo 'Local setup unexpectedly started backup-service' >&2; exit 1; fi
assert_call 'go mod download'
assert_call 'npm ci'
assert_call 'compose.local.yaml up -d --build --wait --wait-timeout 900'
grep -q "^IOT_EMBEDDING_URL='http://127.0.0.1:18091/v1'$" "$test_root/.env.local"
grep -q '^IOT_EMBEDDING_IMAGE=ghcr.io/huggingface/text-embeddings-inference:cpu-1.9$' "$test_root/.env.local"
grep -Eq '^IOT_EMBEDDING_API_KEY=[0-9a-f]{64}$' "$test_root/.env.local"
assert_no_call 'ollama'
cp "$test_root/.env.local" "$test_root/local-original"
bash "$scripts/setup-local.sh" --env-file "$test_root/.env.local" --skip-code-deps
cmp "$test_root/local-original" "$test_root/.env.local"
echo 'PASS local: dependency preparation and unchanged configuration on rerun'

capacity_env="$test_root/.env.local-capacity"
cp "$test_root/.env.local" "$capacity_env"
bash "$scripts/setup-local.sh" --env-file "$capacity_env" --skip-code-deps --capacity off
grep -q "^IOT_CAPACITY_MODULE='off'$" "$capacity_env"
bash "$scripts/setup-local.sh" --env-file "$capacity_env" --skip-code-deps
grep -q "^IOT_CAPACITY_MODULE='off'$" "$capacity_env"
bash "$scripts/setup-local.sh" --env-file "$capacity_env" --skip-code-deps --capacity on
grep -q "^IOT_CAPACITY_MODULE='on'$" "$capacity_env"
if bash "$scripts/setup-local.sh" --capacity invalid >/dev/null 2>&1; then echo 'Accepted invalid capacity switch' >&2; exit 1; fi
echo 'PASS local capacity: default on, explicit opt-out kept, re-enable without a capacity container'

no_harness_env="$test_root/.env.no-harness"
cp "$test_root/.env.local" "$no_harness_env"
sed "s/^IOT_AI_HARNESS_ENABLED=.*/IOT_AI_HARNESS_ENABLED='false'/" "$no_harness_env" > "$test_root/no-harness.tmp"
mv "$test_root/no-harness.tmp" "$no_harness_env"
: > "$TEST_CALLS"
bash "$scripts/setup-local.sh" --env-file "$no_harness_env" --skip-code-deps
grep -q "^IOT_AI_HARNESS_ENABLED='true'$" "$no_harness_env"
grep -q "^IOT_AI_HARNESS_URL='http://127.0.0.1:8091'$" "$no_harness_env"
assert_call 'compose.local.yaml up -d --build --wait'
if bash "$scripts/setup-local.sh" --env-file "$no_harness_env" --skip-code-deps --no-harness 2>/dev/null; then echo 'setup-local accepted --no-harness' >&2; exit 1; fi
echo 'PASS local configuration: Harness is mandatory and a disabled flag is switched back on'

bash "$scripts/setup-local.sh" --env-file "$test_root/.env.remote" --skip-code-deps --dependency-host 192.168.24.133 --api-host 192.168.24.1
grep -q "^IOT_LOCAL_BIND_ADDRESS='0.0.0.0'$" "$test_root/.env.remote"
grep -q "^IOT_LOCAL_ADVERTISED_HOST='192.168.24.133'$" "$test_root/.env.remote"
grep -q "^IOT_POSTGRES_DSN='postgres://.*@192.168.24.133:15432/iot?sslmode=disable'$" "$test_root/.env.remote"
grep -q "^IOT_KAFKA_BROKERS='192.168.24.133:19092'$" "$test_root/.env.remote"
grep -q "^IOT_AI_HARNESS_MCP_URL='http://192.168.24.1:8081/mcp/harness'$" "$test_root/.env.remote"
grep -q "^IOT_HARNESS_MCP_ALLOWED_ORIGINS='http://192.168.24.1:8081'$" "$test_root/.env.remote"
remote_compose="$test_root/remote-compose.yaml"
"$TEST_COMPOSE" --project-name iot-platform-local --env-file "$test_root/.env.remote" -f "$scripts/../compose.local.yaml" config > "$remote_compose"
grep -q 'host_ip: 0.0.0.0' "$remote_compose"
grep -q 'external://192.168.24.133:19092' "$remote_compose"
grep -q 'image: postgres:17-alpine3.22' "$remote_compose"
grep -q 'image: iot-platform-minio:local' "$remote_compose"
grep -q 'context: .*/deploy/minio' "$remote_compose"
grep -q 'IOT_HARNESS_MCP_ALLOWED_ORIGINS: http://192.168.24.1:8081' "$remote_compose"
if grep -q '^  backup-service:' "$remote_compose"; then echo 'Local default Compose unexpectedly includes backup-service' >&2; exit 1; fi
backup_compose="$test_root/remote-backup-compose.yaml"
"$TEST_COMPOSE" --project-name iot-platform-local --env-file "$test_root/.env.remote" -f "$scripts/../compose.local.yaml" --profile backup config > "$backup_compose"
grep -A80 '^  backup-service:' "$backup_compose" | grep -q 'IOT_CLICKHOUSE_URL'
echo 'PASS local remote-host: published dependencies and advertised addresses'

# The VM preset includes middleware and ops; backup remains host-side source.
uname() { if [ "${1:-}" = -s ]; then echo Linux; else command uname "$@"; fi; }
export -f uname
: > "$TEST_CALLS"
vm_env="$test_root/.env.vm"
bash "$scripts/setup-local.sh" --env-file "$vm_env" --dependencies-only --dependency-host 192.168.24.133 --api-host 192.168.24.1 --video on --rtc-ip 192.168.24.133 --transcode
assert_call '--profile ops up -d --build --wait'
assert_call 'stop backup-service'
assert_no_call 'go mod download|npm ci|--profile backup .*up '
grep -q "^IOT_BACKUP_URL='http://127.0.0.1:8092'$" "$vm_env"
grep -q "^IOT_LOCAL_API_HOST='192.168.24.1'$" "$vm_env"
grep -q "^IOT_LOCAL_BACKUP_METRICS_TARGET='192.168.24.1:8092'$" "$vm_env"
grep -q '^IOT_VIDEO_MEDIA_API_URL=http://192.168.24.133:18580$' "$vm_env"
assert_call 'build --pull zlmediakit'
cp "$vm_env" "$test_root/vm-enabled"
bash "$scripts/setup-local.sh" --env-file "$vm_env" --dependencies-only --video off
grep -q "^IOT_BACKUP_URL='http://127.0.0.1:8092'$" "$vm_env"
grep -q "^IOT_LOCAL_API_HOST='192.168.24.1'$" "$vm_env"
grep -q '^IOT_VIDEO_MEDIA_API_URL=$' "$vm_env"
assert_call 'stop zlmediakit'
assert_call 'rm -f zlmediakit'
for key in POSTGRES_PASSWORD IOT_VIDEO_MEDIA_SECRET IOT_VIDEO_HOOK_SECRET IOT_VIDEO_CREDENTIAL_KEY; do
  diff <(grep "^$key=" "$test_root/vm-enabled") <(grep "^$key=" "$vm_env")
done
printf "IOT_LOCAL_OPS_DIR='%s/shared ops'\n" "$test_root" >> "$vm_env"
bash "$scripts/setup-local.sh" --env-file "$vm_env" --dependencies-only --dependency-host 127.0.0.1 --api-host host.orb.internal
test -f "$test_root/shared ops/alertmanager/alertmanager.yml"
grep -Fq "IOT_OPS_ALERTMANAGER_CONFIG_FILE='$test_root/shared ops/alertmanager/alertmanager.yml'" "$vm_env"
"$TEST_COMPOSE" --env-file "$vm_env" -f "$scripts/../compose.local.yaml" --profile ops config > "$test_root/vm-ops.yaml"
grep -Fq "$test_root/shared ops/alertmanager" "$test_root/vm-ops.yaml"
if grep -q '^  backup-service:' "$test_root/vm-ops.yaml"; then echo 'VM dependencies unexpectedly include backup-service' >&2; exit 1; fi
grep -q "^IOT_LOCAL_BACKUP_METRICS_TARGET='host.orb.internal:8092'$" "$vm_env"
# Containers remain an explicit opt-in; rerunning the default releases its port.
: > "$TEST_CALLS"
bash "$scripts/setup-local.sh" --env-file "$vm_env" --dependencies-only --include-backup
assert_call '--profile backup --profile ops up -d --build --wait'
assert_no_call 'stop backup-service'
grep -q "^IOT_LOCAL_BACKUP_METRICS_TARGET='backup-service:8090'$" "$vm_env"
: > "$TEST_CALLS"
bash "$scripts/setup-local.sh" --env-file "$vm_env" --dependencies-only
assert_call 'stop backup-service'
assert_no_call '--profile backup .*up '
grep -q "^IOT_LOCAL_BACKUP_METRICS_TARGET='host.orb.internal:8092'$" "$vm_env"
if bash "$scripts/setup-local.sh" --video invalid >/dev/null 2>&1; then echo 'Accepted invalid video switch' >&2; exit 1; fi
if bash "$scripts/setup-local.sh" --video off --transcode >/dev/null 2>&1; then echo 'Accepted media options without video on' >&2; exit 1; fi
unset -f uname
echo 'PASS VM preset: source backup by default, explicit container opt-in, video toggle and stable configuration'

deepseek_env="$test_root/.env.deepseek"
cp "$test_root/.env.remote" "$deepseek_env"
printf "IOT_AI_API_KEY='smoke-test-key'\n" >> "$deepseek_env"
bash "$scripts/setup-local.sh" --env-file "$deepseek_env" --skip-code-deps --dependency-host 192.168.24.133 --api-host 192.168.24.1
grep -q "^IOT_AI_PROVIDER=deepseek$" "$deepseek_env"
grep -q "^IOT_AI_BASE_URL=https://api.deepseek.com$" "$deepseek_env"
grep -q "^IOT_AI_MODEL=deepseek-flash$" "$deepseek_env"
grep -q "^DEEPSEEK_API_KEY='smoke-test-key'$" "$deepseek_env"
assert_no_call 'ollama|vllm'
echo 'PASS local deepseek: provider enabled without local chat model download'

bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online"
grep -q '^IOT_AI_PROVIDER=deepseek$' "$test_root/.env.online"
grep -q '^IOT_AI_BASE_URL=https://api.deepseek.com$' "$test_root/.env.online"
grep -q '^IOT_AI_MODEL=deepseek-flash$' "$test_root/.env.online"
grep -q '^IOT_AI_HARNESS_ENABLED=true$' "$test_root/.env.online"
grep -q '^IOT_AI_HARNESS_URL=http://deepseek-harness:8091$' "$test_root/.env.online"
grep -q '^IOT_AI_HARNESS_PROVIDER=deepseek-official$' "$test_root/.env.online"
grep -q '^IOT_AI_HARNESS_MODEL=deepseek-flash$' "$test_root/.env.online"
assert_commented_env "$test_root/.env.online"
grep -q '^IOT_ADMIN_PASSWORD=admin123$' "$test_root/.env.online"
assert_call 'build --pull platform-api platform-web backup-service deepseek-harness'
assert_no_call 'ollama'
grep -q '^IOT_EMBEDDING_URL=http://embedding:80/v1$' "$test_root/.env.online"
grep -q '^IOT_EMBEDDING_MODEL=Qwen/Qwen3-Embedding-0.6B$' "$test_root/.env.online"
grep -q '^IOT_EMBEDDING_IMAGE=ghcr.io/huggingface/text-embeddings-inference:cpu-1.9$' "$test_root/.env.online"
grep -Eq '^IOT_EMBEDDING_API_KEY=[0-9a-f]{64}$' "$test_root/.env.online"
grep -q '^IOT_PRIVATE_LLM=off$' "$test_root/.env.online"
assert_call 'pull .*embedding'
assert_call '--profile llm rm -sf vllm'
cp "$test_root/.env.online" "$test_root/online-original"
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online"
cmp "$test_root/online-original" "$test_root/.env.online"
assert_call 'build --pull platform-api platform-web backup-service'
grep -q '8081/health/ready' "$TEST_HTTP"
grep -q '8092/health/ready' "$TEST_HTTP"
TEST_FAIL_BUILD=1
: > "$TEST_CALLS"
if bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online"; then echo 'Build failure ignored' >&2; exit 1; fi
assert_no_call ' up '
TEST_FAIL_BUILD=0
echo 'PASS online: build, health checks, AI, repeatability and failure handling'

: > "$TEST_CALLS"
bash "$scripts/package-offline.sh" --output-dir "$test_root/bundles with spaces"
bundles=("$test_root"/bundles\ with\ spaces/iot-platform-offline-*/)
bundle="${bundles[0]%/}"
bundle_name="$(basename "$bundle")"
(cd "$(dirname "$bundle")" && sha256sum -c "$bundle_name.tar.sha256")
mkdir -p "$test_root/extracted with spaces"
tar -xf "$bundle.tar" -C "$test_root/extracted with spaces"
extracted_bundle="$test_root/extracted with spaces/$bundle_name"
diff -r "$bundle" "$extracted_bundle"
[ -f "$extracted_bundle/.env.offline" ]
bash "$scripts/deploy-offline.sh" --bundle-dir "$extracted_bundle" > "$test_root/extracted-deploy.log"
echo 'PASS complete tar: checksum, hidden config, identical contents and extracted deployment'
assert_commented_env "$bundle/.env.offline"
grep -q '^IOT_ADMIN_PASSWORD=admin123$' "$bundle/.env.offline"
[ -s "$bundle/embedding-models.tgz.sha256" ]
[ -s "$bundle/docker-runtime/docker-24.0.9.tgz.sha256" ]
[ -s "$bundle/docker-runtime/docker-28.5.2.tgz.sha256" ]
[ -s "$bundle/docker-runtime/docker-compose.sha256" ]
[ -f "$bundle/scripts/lib/docker-bootstrap.sh" ]
[ -f "$bundle/scripts/lib/restore-volume-archive.sh" ]
grep -q 'text-embeddings-inference:cpu-1.9' "$bundle/manifest.json"
grep -q '"embeddingModel": "Qwen/Qwen3-Embedding-0.6B"' "$bundle/manifest.json"
grep -q '"arch": "x86_64"' "$bundle/manifest.json"
if grep -qi 'ollama' "$bundle/manifest.json" "$bundle/.env.offline"; then echo 'Offline bundle still references Ollama' >&2; exit 1; fi
grep -q 'weaviate:' "$bundle/manifest.json"
grep -q 'iot-platform-minio:RELEASE.2025-09-07T16-13-09Z' "$bundle/manifest.json"
assert_call 'build --pull platform-api platform-web backup-service minio'
assert_call 'up -d --no-deps embedding'
assert_call 'exec -T embedding curl'
assert_call 'export-model-cache.sh /src Qwen/Qwen3-Embedding-0.6B /backup/embedding-models.tgz'
assert_no_call 'ollama|vllm'
grep -q '^HF_HUB_OFFLINE=1$' "$bundle/.env.offline"
grep -q '^IOT_EMBEDDING_MODEL_SOURCE=/data/offline/Qwen3-Embedding-0.6B$' "$bundle/.env.offline"
grep -q '^IOT_PRIVATE_LLM=off$' "$bundle/.env.offline"
if grep -qx 'llm' "$bundle/profiles.txt"; then echo 'Default bundle includes the private LLM' >&2; exit 1; fi
grep -q '^IOT_AI_PROVIDER=deepseek$' "$bundle/.env.offline"
grep -q '^IOT_AI_MODEL=deepseek-flash$' "$bundle/.env.offline"
grep -q '^IOT_AI_HARNESS_PROVIDER=deepseek-official$' "$bundle/.env.offline"
grep -qx 'harness' "$bundle/profiles.txt"
cp "$bundle/.env.offline" "$test_root/offline-original"
: > "$TEST_CALLS"
bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"
bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"
cmp "$test_root/offline-original" "$bundle/.env.offline"
assert_call 'up -d --no-build --pull never --wait --wait-timeout 900'
assert_call 'restore-volume-archive.sh /backup/embedding-models.tgz /dst'
assert_call 'exec -T embedding curl'
assert_no_call ' build |ollama| compose .* pull '
TEST_MISSING_IMAGE=1
: > "$TEST_CALLS"
if bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle" > "$test_root/missing-image.log" 2>&1; then echo 'Missing image ignored' >&2; exit 1; fi
grep -q '离线包缺少镜像：' "$test_root/missing-image.log"
if grep -q 'unbound variable' "$test_root/missing-image.log"; then echo 'Missing image diagnostic failed'; exit 1; fi
assert_no_call ' up '
TEST_MISSING_IMAGE=0
python3 "$scripts/prepare-public-bundle.py" "$bundle"
[ ! -e "$bundle/.env.offline" ]
# Camera and GB28181 passwords are sealed with a key generated on the target.
grep -q '^IOT_VIDEO_CREDENTIAL_KEY=__TORCHLINK_RANDOM_BASE64_32__$' "$bundle/.env.offline.template"
grep -q '^IOT_EMBEDDING_API_KEY=__TORCHLINK_RANDOM_HEX__$' "$bundle/.env.offline.template"
grep -q '^IOT_LLM_API_KEY=__TORCHLINK_RANDOM_HEX__$' "$bundle/.env.offline.template"
: > "$TEST_CALLS"
bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"
[ -s "$bundle/.env.offline" ]
if grep -q '__TORCHLINK_RANDOM_' "$bundle/.env.offline"; then echo 'Public template was not initialized'; exit 1; fi
grep -Eq '^IOT_VIDEO_CREDENTIAL_KEY=[A-Za-z0-9+/]{43}=$' "$bundle/.env.offline"
assert_call 'up -d --no-build --pull never'
assert_no_call ' build |ollama| compose .* pull '
cp "$bundle/.env.offline" "$test_root/public-original"
bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"
cmp "$test_root/public-original" "$bundle/.env.offline"
echo 'PASS public bundle: target credentials, real Compose parsing and unchanged configuration on rerun'
printf 'corruption' >> "$bundle/images.tar"
: > "$TEST_CALLS"
if bash "$scripts/deploy-offline.sh" --bundle-dir "$bundle"; then echo 'Corrupt archive accepted' >&2; exit 1; fi
assert_no_call '^load '
echo 'PASS offline: complete bundle, no network, repeatability and corrupt/missing image checks'

# Camera live module: packaged and deployed by default; an explicit opt-out is kept.
grep -qx 'video' "$bundle/profiles.txt"
grep -q '^IOT_VIDEO_MODULE=on$' "$test_root/offline-original"
grep -q '^IOT_CAPACITY_MODULE=on$' "$test_root/offline-original"
grep -q '^IOT_OPS_CAPACITY_URL=http://capacity:7080$' "$test_root/offline-original"
grep -Eq '^COMPOSE_PROFILES=(video,capacity|capacity,video)$' "$test_root/.env.online"
grep -q '^IOT_VIDEO_MEDIA_API_URL=http://zlmediakit:80$' "$test_root/.env.online"
: > "$TEST_CALLS"
bash "$scripts/package-offline.sh" --output-dir "$test_root/video-bundles" --skip-embedding-model --skip-docker-runtime
vbundles=("$test_root"/video-bundles/iot-platform-offline-*)
vbundle="${vbundles[0]}"
grep -qx 'video' "$vbundle/profiles.txt"
grep -q 'iot-zlmediakit:offline' "$vbundle/manifest.json"
grep -q '^IOT_VIDEO_MEDIA_API_URL=http://zlmediakit:80$' "$vbundle/.env.offline"
for key in IOT_VIDEO_MEDIA_SECRET IOT_VIDEO_HOOK_SECRET IOT_VIDEO_CREDENTIAL_KEY; do grep -Eq "^$key=.{24,}$" "$vbundle/.env.offline"; done
assert_commented_env "$vbundle/.env.offline"
assert_call '--profile video build --pull zlmediakit'
[ -f "$vbundle/scripts/video-module.sh" ] && [ -f "$vbundle/scripts/video-module.ps1" ] && [ -f "$vbundle/scripts/lib/deployment.sh" ]
: > "$TEST_CALLS"
bash "$scripts/deploy-offline.sh" --bundle-dir "$vbundle" > "$test_root/video-deploy.log"
assert_call '--profile video'
grep -q 'IOT_VIDEO_RTC_EXTERN_IP' "$test_root/video-deploy.log"
video_key="$(grep '^IOT_VIDEO_CREDENTIAL_KEY=' "$vbundle/.env.offline")"
bash "$vbundle/scripts/video-module.sh" disable --mode offline --env-file "$vbundle/.env.offline" > /dev/null
grep -q '^IOT_VIDEO_MEDIA_API_URL=$' "$vbundle/.env.offline"
grep -q '^IOT_VIDEO_MODULE=off$' "$vbundle/.env.offline"
assert_call 'rm -f zlmediakit'
: > "$TEST_CALLS"
bash "$scripts/deploy-offline.sh" --bundle-dir "$vbundle" > /dev/null
assert_no_call '--profile video'
bash "$vbundle/scripts/video-module.sh" enable --mode offline --env-file "$vbundle/.env.offline" --rtc-ip 192.0.2.10 > /dev/null
grep -q '^IOT_VIDEO_RTC_EXTERN_IP=192.0.2.10$' "$vbundle/.env.offline"
grep -q '^IOT_VIDEO_MEDIA_API_URL=http://zlmediakit:80$' "$vbundle/.env.offline"
grep -q '^IOT_VIDEO_MODULE=on$' "$vbundle/.env.offline"
[ "$video_key" = "$(grep '^IOT_VIDEO_CREDENTIAL_KEY=' "$vbundle/.env.offline")" ] || { echo 'Camera credential key was rotated' >&2; exit 1; }
assert_call 'up -d --no-build --pull never --wait --wait-timeout 120 zlmediakit'
: > "$TEST_CALLS"
bash "$scripts/package-offline.sh" --output-dir "$test_root/novideo-bundles" --without-video --skip-embedding-model --skip-docker-runtime --skip-bundle-archive > /dev/null
nbundles=("$test_root"/novideo-bundles/iot-platform-offline-*)
if grep -qx 'video' "${nbundles[0]}/profiles.txt"; then echo 'Opt-out bundle includes video' >&2; exit 1; fi
grep -q '^IOT_VIDEO_MODULE=off$' "${nbundles[0]}/.env.offline"
assert_no_call 'zlmediakit'
[ ! -e "${nbundles[0]}.tar" ] && [ ! -e "${nbundles[0]}.tar.sha256" ]
# A failed tar must not publish a completed archive or leave partial artifacts.
tar() { printf 'partial archive' > "$2"; return 48; }
export -f tar
if bash "$scripts/package-offline.sh" --output-dir "$test_root/failed-tar" --skip-embedding-model --skip-docker-runtime > "$test_root/failed-tar.log" 2>&1; then
  echo 'Archive failure ignored' >&2; exit 1
fi
unset -f tar
failed_bundles=("$test_root"/failed-tar/iot-platform-offline-*/)
[ -f "${failed_bundles[0]}manifest.json" ]
[ -z "$(find "$test_root/failed-tar" -maxdepth 1 -type f -print)" ]
echo 'PASS complete tar: explicit opt-out and cleanup after archive failure'
# Online: --video off is kept by later default deployments; --video on restores it.
cp "$test_root/.env.online" "$test_root/.env.online-video"
: > "$TEST_CALLS"
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online-video" --video off > /dev/null
grep -q '^IOT_VIDEO_MEDIA_API_URL=$' "$test_root/.env.online-video"
grep -q '^COMPOSE_PROFILES=capacity$' "$test_root/.env.online-video"
assert_call '--profile video rm -sf zlmediakit'
assert_no_call 'build --pull .*zlmediakit'
: > "$TEST_CALLS"
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online-video" > /dev/null
grep -q '^IOT_VIDEO_MODULE=off$' "$test_root/.env.online-video"
assert_no_call 'build --pull .*zlmediakit'
: > "$TEST_CALLS"
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online-video" --video on > /dev/null
grep -Eq '^COMPOSE_PROFILES=(video,capacity|capacity,video)$' "$test_root/.env.online-video"
assert_call 'build --pull platform-api platform-web backup-service deepseek-harness zlmediakit'
assert_no_call ' pull .*zlmediakit'
"$TEST_COMPOSE" --env-file "$test_root/.env.online-video" -f "$scripts/../compose.yaml" config > "$test_root/video-online.yaml"
grep -q 'published: "5060"' "$test_root/video-online.yaml"
grep -q 'published: "30063"' "$test_root/video-online.yaml"
echo 'PASS video module: default on, GB28181 ports, offline packaging, opt-out kept and online toggle'

# Capacity-test module: deployed by default from the platform image with a
# generated token; --capacity off is kept by later deployments; the module
# script and the offline switch toggle it.
cp "$test_root/.env.online" "$test_root/.env.online-capacity"
: > "$TEST_CALLS"
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online-capacity" > /dev/null
grep -q '^IOT_CAPACITY_MODULE=on$' "$test_root/.env.online-capacity"
grep -q '^IOT_OPS_CAPACITY_URL=http://capacity:7080$' "$test_root/.env.online-capacity"
grep -Eq '^IOT_OPS_CAPACITY_TOKEN=.{32,}$' "$test_root/.env.online-capacity"
grep -Eq '^COMPOSE_PROFILES=(video,capacity|capacity,video)$' "$test_root/.env.online-capacity"
assert_no_call ' pull .*capacity'
assert_no_call 'rm -sf capacity'
assert_commented_env "$test_root/.env.online-capacity"
capacity_token="$(grep '^IOT_OPS_CAPACITY_TOKEN=' "$test_root/.env.online-capacity" | cut -d= -f2-)"
"$TEST_COMPOSE" --env-file "$test_root/.env.online-capacity" -f "$scripts/../compose.yaml" config > "$test_root/capacity-online.yaml"
grep -q '/app/capacity-test' "$test_root/capacity-online.yaml"
grep -q "IOT_CAPACITY_SERVICE_TOKEN: $capacity_token" "$test_root/capacity-online.yaml"
grep -q "IOT_OPS_CAPACITY_TOKEN: $capacity_token" "$test_root/capacity-online.yaml"
if grep -q 'published: "7080"' "$test_root/capacity-online.yaml"; then echo 'Capacity service must not publish a host port' >&2; exit 1; fi
: > "$TEST_CALLS"
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online-capacity" --capacity off > /dev/null
grep -q '^IOT_CAPACITY_MODULE=off$' "$test_root/.env.online-capacity"
grep -q '^IOT_OPS_CAPACITY_URL=$' "$test_root/.env.online-capacity"
assert_call '--profile capacity rm -sf capacity'
: > "$TEST_CALLS"
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online-capacity" > /dev/null
grep -q '^IOT_CAPACITY_MODULE=off$' "$test_root/.env.online-capacity"
assert_call '--profile capacity rm -sf capacity'
: > "$TEST_CALLS"
bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online-capacity" --capacity on > /dev/null
grep -q '^IOT_CAPACITY_MODULE=on$' "$test_root/.env.online-capacity"
[ "$capacity_token" = "$(grep '^IOT_OPS_CAPACITY_TOKEN=' "$test_root/.env.online-capacity" | cut -d= -f2-)" ] || { echo 'Capacity token was rotated' >&2; exit 1; }
: > "$TEST_CALLS"
bash "$scripts/capacity-module.sh" disable --mode online --env-file "$test_root/.env.online-capacity" > /dev/null
grep -q '^IOT_CAPACITY_MODULE=off$' "$test_root/.env.online-capacity"
grep -q '^IOT_OPS_CAPACITY_URL=$' "$test_root/.env.online-capacity"
assert_call 'rm -f capacity'
assert_call 'up -d --no-deps --no-build platform-api'
: > "$TEST_CALLS"
bash "$scripts/capacity-module.sh" enable --mode online --env-file "$test_root/.env.online-capacity" > /dev/null
grep -q '^IOT_CAPACITY_MODULE=on$' "$test_root/.env.online-capacity"
assert_call 'up -d --no-build --pull never capacity'
if bash "$scripts/capacity-module.sh" enable --mode local --env-file "$test_root/.env.online-capacity" >/dev/null 2>&1; then echo 'Capacity module accepted local mode' >&2; exit 1; fi
if bash "$scripts/deploy-online.sh" --env-file "$test_root/.env.online-capacity" --capacity maybe >/dev/null 2>&1; then echo 'Accepted invalid capacity switch' >&2; exit 1; fi
[ -f "$vbundle/scripts/capacity-module.sh" ] && [ -f "$vbundle/scripts/capacity-module.ps1" ]
: > "$TEST_CALLS"
bash "$scripts/deploy-offline.sh" --bundle-dir "$vbundle" > /dev/null
grep -q '^IOT_CAPACITY_MODULE=on$' "$vbundle/.env.offline"
grep -Eq '^IOT_OPS_CAPACITY_TOKEN=.{32,}$' "$vbundle/.env.offline"
assert_call '--profile capacity'
: > "$TEST_CALLS"
bash "$scripts/deploy-offline.sh" --bundle-dir "$vbundle" --capacity off > /dev/null
grep -q '^IOT_CAPACITY_MODULE=off$' "$vbundle/.env.offline"
assert_call '--profile capacity rm -sf capacity'
: > "$TEST_CALLS"
bash "$scripts/deploy-offline.sh" --bundle-dir "$vbundle" > /dev/null
grep -q '^IOT_CAPACITY_MODULE=off$' "$vbundle/.env.offline"
echo 'PASS capacity module: default on, generated token, opt-out kept, module toggle and offline switch'
# Optional private chat model (vLLM): off by default, opt-in kept, packaged on request.
llm_env="$test_root/.env.online-llm"
cp "$test_root/.env.online" "$llm_env"
: > "$TEST_CALLS"
bash "$scripts/deploy-online.sh" --env-file "$llm_env" --private-llm on > /dev/null
grep -q '^IOT_PRIVATE_LLM=on$' "$llm_env"
grep -Eq '^COMPOSE_PROFILES=.*llm' "$llm_env"
grep -q '^IOT_LLM_MODEL=Qwen/Qwen3-8B$' "$llm_env"
assert_call 'pull .*vllm'
assert_no_call 'rm -sf vllm'
"$TEST_COMPOSE" --env-file "$llm_env" -f "$scripts/../compose.yaml" config > "$test_root/llm-online.yaml"
grep -q 'vllm/vllm-openai:v0.29.0' "$test_root/llm-online.yaml"
grep -q 'driver: nvidia' "$test_root/llm-online.yaml"
grep -q -- '--enable-auto-tool-choice' "$test_root/llm-online.yaml"
: > "$TEST_CALLS"
bash "$scripts/deploy-online.sh" --env-file "$llm_env" > /dev/null
grep -q '^IOT_PRIVATE_LLM=on$' "$llm_env"
bash "$scripts/deploy-online.sh" --env-file "$llm_env" --private-llm off > /dev/null
grep -q '^IOT_PRIVATE_LLM=off$' "$llm_env"
if grep -Eq '^COMPOSE_PROFILES=.*llm' "$llm_env"; then echo 'Private LLM profile kept after opt-out' >&2; exit 1; fi
assert_call '--profile llm rm -sf vllm'
if bash "$scripts/deploy-online.sh" --private-llm maybe >/dev/null 2>&1; then echo 'Accepted invalid private LLM switch' >&2; exit 1; fi
: > "$TEST_CALLS"
bash "$scripts/package-offline.sh" --output-dir "$test_root/llm-bundles" --with-private-llm --skip-docker-runtime --skip-bundle-archive > /dev/null
lbundles=("$test_root"/llm-bundles/iot-platform-offline-*)
lbundle="${lbundles[0]}"
grep -qx 'llm' "$lbundle/profiles.txt"
[ -s "$lbundle/llm-models.tgz.sha256" ] && [ -s "$lbundle/embedding-models.tgz.sha256" ]
grep -q '^IOT_LLM_MODEL_SOURCE=/root/.cache/huggingface/offline/Qwen3-8B$' "$lbundle/.env.offline"
grep -q 'vllm/vllm-openai' "$lbundle/manifest.json"
assert_call 'snapshot_download'
assert_call 'export-model-cache.sh /src/hub Qwen/Qwen3-8B /backup/llm-models.tgz'
assert_no_call 'compose .* run .*vllm'
: > "$TEST_CALLS"
bash "$scripts/deploy-offline.sh" --bundle-dir "$lbundle" > /dev/null
assert_call '--profile llm'
assert_call 'restore-volume-archive.sh /backup/llm-models.tgz /dst'
sed 's/^IOT_PRIVATE_LLM=on$/IOT_PRIVATE_LLM=off/' "$lbundle/.env.offline" > "$test_root/llm-off.tmp" && cat "$test_root/llm-off.tmp" > "$lbundle/.env.offline"
: > "$TEST_CALLS"
bash "$scripts/deploy-offline.sh" --bundle-dir "$lbundle" > /dev/null
assert_no_call '--profile llm'
if bash "$scripts/package-offline.sh" --output-dir "$test_root/legacy-flag" --skip-ollama-model > "$test_root/legacy-flag.log" 2>&1; then echo 'Accepted removed Ollama flag' >&2; exit 1; fi
grep -q 'Ollama 已移除' "$test_root/legacy-flag.log"
echo 'PASS private LLM: off by default, opt-in kept, GPU service, offline weights and opt-out'
echo 'Bash deployment smoke tests PASS (mutations mocked; Compose parsing real).'
