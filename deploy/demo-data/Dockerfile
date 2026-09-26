FROM node:22-alpine

WORKDIR /workspace
# Reuse the project's locked MQTT dependency; no npm installation at runtime.
COPY iot_front/package.json iot_front/package-lock.json ./iot_front/
RUN npm --prefix iot_front ci --omit=dev --ignore-scripts --no-audit --no-fund \
    && npm cache clean --force

COPY scripts/generate-demo-data.mjs ./scripts/
COPY scripts/lib/demo-config.mjs scripts/lib/demo-samples.mjs ./scripts/lib/
COPY scripts/tests/demo-platform.mjs scripts/tests/demo-devices.mjs scripts/tests/demo-mqtt-device.mjs ./scripts/tests/
COPY internal/protocolbuild/functiontemplates/tcp.go.txt ./internal/protocolbuild/functiontemplates/

# Check the runtime, module resolution and bundled Go template during packaging.
RUN node scripts/generate-demo-data.mjs --origin http://127.0.0.1:1 --dry-run \
    && node --input-type=module -e "import {createRequire} from 'node:module'; import {readFileSync} from 'node:fs'; createRequire('/workspace/iot_front/package.json')('mqtt'); readFileSync('internal/protocolbuild/functiontemplates/tcp.go.txt')"

ENTRYPOINT ["node", "scripts/generate-demo-data.mjs"]
