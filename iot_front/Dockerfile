FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
# Upstreams are substituted at start (nginx template envsubst); a cluster sets
# them to the API load balancer and the media node.
ENV IOT_API_UPSTREAM=platform-api:8080 IOT_VIDEO_UPSTREAM=zlmediakit:80
COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY docker/15-torchlink-tls.sh /docker-entrypoint.d/15-torchlink-tls.sh
COPY docker/security-headers.conf /etc/nginx/snippets/torchlink-security.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080 8443
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O - http://127.0.0.1:8080/health/live >/dev/null || exit 1
CMD ["nginx", "-g", "daemon off;"]
