FROM node:22-alpine@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402 AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# 以非 root 用户（uid 101）运行，只监听 8080 / 8443。
FROM nginxinc/nginx-unprivileged:1.27-alpine@sha256:65e3e85dbaed8ba248841d9d58a899b6197106c23cb0ff1a132b7bfe0547e4c0
# Upstreams are substituted at start (nginx template envsubst); a cluster sets
# them to the API load balancer and the media node.
ENV IOT_API_UPSTREAM=platform-api:8080 IOT_VIDEO_UPSTREAM=zlmediakit:80
COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY docker/15-torchlink-tls.sh /docker-entrypoint.d/15-torchlink-tls.sh
COPY docker/security-headers.conf /etc/nginx/snippets/torchlink-security.conf
COPY --from=build /app/dist /usr/share/nginx/html
# The TLS entrypoint writes its listen and redirect snippets here at start.
USER root
RUN mkdir -p /etc/nginx/torchlink && chown nginx:nginx /etc/nginx/torchlink
USER 101
EXPOSE 8080 8443
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O - http://127.0.0.1:8080/health/live >/dev/null || exit 1
CMD ["nginx", "-g", "daemon off;"]
