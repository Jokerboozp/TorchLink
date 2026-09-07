# syntax=docker/dockerfile:1.7

FROM node:24-bookworm-slim AS build

ARG DSH_REVISION=a66e4702047846cdaa10c66c9d3df3951f5ea70d
WORKDIR /harness

RUN npm install --global pnpm@11.7.0
COPY upstream/deepseek-harness/ /harness/
COPY upstream/deepseek-harness.revision /tmp/deepseek-harness.revision
RUN test "$(tr -d '\r\n' < /tmp/deepseek-harness.revision)" = "$DSH_REVISION"
RUN CI=1 pnpm install --frozen-lockfile
# The Docker context intentionally excludes the upstream .git directory. The
# full build accepts this explicit revision for its embedded client metadata.
ENV DSH_CLIENT_COMMIT_HASH=$DSH_REVISION
RUN pnpm run build
COPY deploy/deepseek-harness/prepare-runtime-manifest.mjs /tmp/prepare-runtime-manifest.mjs
COPY deploy/deepseek-harness/prepare-runtime-carrier.mjs /tmp/prepare-runtime-carrier.mjs
RUN node /tmp/prepare-runtime-manifest.mjs /harness/python/sdk-runtime/package.json
RUN CI=1 pnpm --filter dsh-python-runtime-closure deploy \
    --legacy \
    --prod \
    --config.node-linker=hoisted \
    --config.auto-install-peers=false \
    --config.link-workspace-packages=true \
    /harness/runtime-node
RUN node /tmp/prepare-runtime-carrier.mjs /harness /harness/runtime-node

COPY deploy/deepseek-harness/ /harness/examples/iot-ops-agent/
RUN sed -i 's/\r$//' /harness/examples/iot-ops-agent/*.sh
RUN test "$(tr -d '\r\n' < /harness/examples/iot-ops-agent/REVISION)" = "$DSH_REVISION"
RUN node --test /harness/examples/iot-ops-agent/gateway.test.mjs
RUN node --input-type=module -e "await import('/harness/runtime-node/node_modules/@deepseek-ai/dsh-sdk-minimal/lib/index.js'); await import('/harness/runtime-node/node_modules/@deepseek-ai/dsh-llm-pi-ai/lib/index.js'); await import('/harness/runtime-node/node_modules/@deepseek-ai/dsh-system-prompt/lib/index.js'); await import('/harness/runtime-node/node_modules/@deepseek-ai/dsh-agent-loop/lib/index.js'); await import('/harness/runtime-node/node_modules/@deepseek-ai/dsh-tools/lib/index.js'); await import('/harness/runtime-node/node_modules/@deepseek-ai/dsh-mcp-client/lib/index.js')"
RUN node /harness/examples/iot-ops-agent/runtime-smoke.mjs

FROM node:24-bookworm-slim AS runtime

ARG DSH_REVISION=a66e4702047846cdaa10c66c9d3df3951f5ea70d
LABEL org.opencontainers.image.title="IoT DeepSeek Harness sidecar" \
      org.opencontainers.image.revision="$DSH_REVISION" \
      org.opencontainers.image.source="https://github.com/deepseek-ai/deepseek-harness"

ENV NODE_ENV=production \
    IOT_HARNESS_HOST=0.0.0.0 \
    IOT_HARNESS_PORT=8091 \
    IOT_HARNESS_MCP_PROXY_PORT=8092 \
    IOT_HARNESS_PLUGIN_DIR=/data/plugins \
    IOT_HARNESS_PLUGIN_SEED_DIR=/harness/examples/iot-ops-agent/plugins \
    IOT_HARNESS_SESSION_ROOT=/data/sessions \
    IOT_HARNESS_HOME=/data/runtime-home \
    IOT_HARNESS_WORKSPACE=/data/workspace \
    IOT_HARNESS_PATCH_FILE=/harness/examples/iot-ops-agent/cordis.yml \
    DSH_RUNTIME_CWD=/harness/runtime-node \
    DSH_RUNTIME_BIN=/harness/runtime-node/node_modules/@deepseek-ai/dsh/lib/bin.js \
    DSH_SDK_CLIENT_MODULE=/harness/runtime-node/node_modules/@deepseek-ai/dsh-sdk-client/lib/index.js

WORKDIR /harness/examples/iot-ops-agent
COPY --from=build /harness/runtime-node /harness/runtime-node
COPY --from=build /harness/examples/iot-ops-agent /harness/examples/iot-ops-agent
RUN mkdir -p /data/sessions /data/runtime-home /data/workspace \
    && chown -R node:node /data

USER node
EXPOSE 8091
VOLUME ["/data"]
HEALTHCHECK --interval=15s --timeout=3s --start-period=30s --retries=4 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:8091/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"]
ENTRYPOINT ["sh", "/harness/examples/iot-ops-agent/start.sh"]
