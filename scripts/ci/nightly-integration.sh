#!/usr/bin/env bash
# Nightly integration tests against real brokers and the isolated protocol
# runner. Everything runs in throwaway containers on alternate ports:
#   - EMQX and Redpanda as defined in compose.yaml (JWT/ACL and SASL exactly as
#     deployed), Redpanda's admin API published for the consumer admin test;
#   - a plain Redpanda dev container for the split-process recovery test;
#   - Mosquitto, started by the MQTT inbox test itself;
#   - the protocol runner image, probed from a Go container sharing its socket.
# Needs docker and go on the host. IOT_TEST_POSTGRES_DSN enables the
# PostgreSQL + Kafka split-process test.
set -Eeuo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$root"
project="${IOT_NIGHTLY_PROJECT:-torchlink-nightly}"
random() { head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'; }

export IOT_JWT_SECRET="$(random 32)" IOT_ADMIN_PASSWORD="$(random 12)" IOT_AI_HARNESS_TOKEN="$(random 32)" IOT_BACKUP_ADMIN_TOKEN="$(random 32)"
export IOT_EMQX_API_KEY=nightly IOT_EMQX_API_SECRET="$(random 16)"
export IOT_MQTT_TOOL_USERNAME=admin IOT_MQTT_TOOL_PASSWORD="$(random 16)"
export IOT_KAFKA_ADMIN_USERNAME=nightly-admin IOT_KAFKA_ADMIN_PASSWORD="$(random 16)" IOT_KAFKA_SASL_MECHANISM=SCRAM-SHA-256
export MQTT_PORT=21883 MQTT_WS_PORT=28083 MQTTS_PORT=28883 MQTT_WSS_PORT=28084 EMQX_DASHBOARD_PORT=28183 KAFKA_PORT=29092
override="$(mktemp)"
cat > "$override" <<'YAML'
services:
  redpanda:
    ports: ["127.0.0.1:29644:9644"]
YAML
compose=(docker compose -p "$project" -f compose.yaml -f "$override")
runner_volume="$project-runner"
cleanup() {
  "${compose[@]}" down -v -t 1 >/dev/null 2>&1 || true
  docker rm -f "$project-kafka" "$project-runner" >/dev/null 2>&1 || true
  docker volume rm "$runner_volume" >/dev/null 2>&1 || true
  rm -f "$override"
}
trap cleanup EXIT

echo '::group::启动 EMQX、Redpanda（compose.yaml）与无认证 Redpanda'
"${compose[@]}" up -d --wait --wait-timeout 240 emqx redpanda
docker run -d --name "$project-kafka" -p 127.0.0.1:29192:29192 redpandadata/redpanda:v25.2.11 \
  redpanda start --mode dev-container --smp 1 --kafka-addr PLAINTEXT://0.0.0.0:29192 --advertise-kafka-addr PLAINTEXT://127.0.0.1:29192 >/dev/null
for i in $(seq 1 60); do docker exec "$project-kafka" rpk cluster health --exit-when-healthy >/dev/null 2>&1 && break; sleep 2; done
echo '::endgroup::'

echo '::group::MQTT（EMQX JWT/ACL 与 Mosquitto）'
IOT_TEST_MQTT_BROKER=tcp://127.0.0.1:21883 IOT_TEST_MQTT_JWT_SECRET="$IOT_JWT_SECRET" IOT_TEST_MQTT_STRICT_IDENTITY=true \
IOT_TEST_EMQX_API_URL=http://127.0.0.1:28183 IOT_TEST_EMQX_API_KEY="$IOT_EMQX_API_KEY" IOT_TEST_EMQX_API_SECRET="$IOT_EMQX_API_SECRET" \
IOT_TEST_MESSAGE_TOPICS_MQTT_BROKER=tcp://127.0.0.1:21883 IOT_TEST_MESSAGE_TOPICS_JWT_SECRET="$IOT_JWT_SECRET" \
IOT_TEST_MESSAGE_TOPICS_EMQX_URL=http://127.0.0.1:28183 IOT_TEST_MESSAGE_TOPICS_EMQX_KEY="$IOT_EMQX_API_KEY" IOT_TEST_MESSAGE_TOPICS_EMQX_SECRET="$IOT_EMQX_API_SECRET" \
IOT_TEST_MQTT_TOOL_USERNAME="$IOT_MQTT_TOOL_USERNAME" IOT_TEST_MQTT_TOOL_PASSWORD="$IOT_MQTT_TOOL_PASSWORD" \
IOT_TEST_MQTT_DOCKER=1 \
  go test -count=1 -run 'MQTT|Broker' ./internal/httpapi ./internal/adapters/mqtt ./internal/messagetopics
echo '::endgroup::'

echo '::group::Kafka（SASL 与无认证 Redpanda）'
IOT_TEST_SECURED_KAFKA_BROKERS=127.0.0.1:29092 IOT_TEST_SECURED_KAFKA_USERNAME="$IOT_KAFKA_ADMIN_USERNAME" IOT_TEST_SECURED_KAFKA_PASSWORD="$IOT_KAFKA_ADMIN_PASSWORD" \
IOT_TEST_SECURED_KAFKA_MECHANISM="$IOT_KAFKA_SASL_MECHANISM" IOT_TEST_SECURED_KAFKA_ADMIN_URL=http://127.0.0.1:29644 \
IOT_TEST_MESSAGE_TOPICS_KAFKA_BROKERS=127.0.0.1:29092 IOT_TEST_MESSAGE_TOPICS_KAFKA_USERNAME="$IOT_KAFKA_ADMIN_USERNAME" \
IOT_TEST_MESSAGE_TOPICS_KAFKA_PASSWORD="$IOT_KAFKA_ADMIN_PASSWORD" IOT_TEST_MESSAGE_TOPICS_KAFKA_MECHANISM="$IOT_KAFKA_SASL_MECHANISM" \
  go test -count=1 -run 'Secured|DeadLetters|Kafka' ./internal/adapters/kafka/... ./internal/messagetopics
if [ -n "${IOT_TEST_POSTGRES_DSN:-}" ]; then
  IOT_TEST_DISPOSABLE_KAFKA=127.0.0.1:29192 go test -count=1 -run TestSplitProcessesPostgresKafkaRecovery ./internal/platformapp
fi
echo '::endgroup::'

echo '::group::协议运行器隔离'
image="${IOT_NIGHTLY_PLATFORM_IMAGE:-$project-api:latest}"
[ -n "${IOT_NIGHTLY_PLATFORM_IMAGE:-}" ] || docker build -q -t "$image" . >/dev/null
docker volume create "$runner_volume" >/dev/null
# Same isolation as the protocol-runner service in compose.yaml.
docker run -d --name "$project-runner" --network none --read-only --cap-drop ALL --security-opt no-new-privileges:true \
  --pids-limit 512 --memory 2g --tmpfs /tmp:size=2g,mode=1777,exec \
  -e IOT_PROCESS_ROLE=protocol-runner -e IOT_PROTOCOL_RUNNER_SOCKET=/run/torchlink/runner.sock -e IOT_PROTOCOL_RUNNER_DIR=/tmp/protocol-runner \
  -v "$runner_volume:/run/torchlink" "$image" >/dev/null
for i in $(seq 1 30); do docker exec "$project-runner" /app/iot-platform runner-healthcheck && break; sleep 1; done
# The socket belongs to the runner's non-root user; the test connects from a Go container.
docker run --rm -v "$runner_volume:/run/torchlink" -v "$root:/src:ro" -w /src \
  -e IOT_TEST_PROTOCOL_RUNNER_SOCKET=/run/torchlink/runner.sock -e GOCACHE=/tmp/gocache -e GOFLAGS=-mod=mod \
  "golang:$(sed -n 's/^go \([0-9]*\.[0-9]*\).*/\1/p' go.mod)-alpine" go test -count=1 -run TestContainerRunner ./internal/protocolrunner
echo '::endgroup::'
