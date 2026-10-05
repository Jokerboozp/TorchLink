# Backup service image, built by every Compose file and cluster deployment
# (context: repository root).
FROM golang:1.26-alpine@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/backup-service ./cmd/backup-service

FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8
# pg_dump / pg_restore must be at least the server major version (PostgreSQL 17).
RUN apk add --no-cache ca-certificates tzdata postgresql17-client su-exec
COPY --from=build /out/backup-service /usr/local/bin/backup-service
COPY deploy/backup/entrypoint.sh /usr/local/bin/backup-entrypoint
ENTRYPOINT ["backup-entrypoint"]
