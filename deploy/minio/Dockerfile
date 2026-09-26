FROM alpine:3.22 AS download
ARG TARGETARCH
RUN apk add --no-cache ca-certificates curl
WORKDIR /out
# Keep the existing server release. Official registry images are no longer public.
# Checksums are from the matching upstream GitHub Release checksum assets.
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) server_sha=7c5bd8512c6e966455b1d198209358b2d191c77a83ab377c4073281065fb855f; \
             client_sha=01f866e9c5f9b87c2b09116fa5d7c06695b106242d829a8bb32990c00312e891 ;; \
      arm64) server_sha=5c83cd2cf151717ba0243f73e1c7802ff36e272b67144bdd7f1f7d684fd6f03d; \
             client_sha=14c8c9616cfce4636add161304353244e8de383b2e2752c0e9dad01d4c27c12c ;; \
      *) echo "Unsupported MinIO architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    curl -fSL --retry 3 -o minio "https://github.com/minio/minio/releases/download/RELEASE.2025-09-07T16-13-09Z/minio.linux-$TARGETARCH.RELEASE.2025-09-07T16-13-09Z"; \
    curl -fSL --retry 3 -o mc "https://github.com/minio/mc/releases/download/RELEASE.2025-08-13T08-35-41Z/mc.linux-$TARGETARCH.RELEASE.2025-08-13T08-35-41Z"; \
    printf '%s  minio\n%s  mc\n' "$server_sha" "$client_sha" | sha256sum -c -; \
    chmod 755 minio mc; \
    ./minio --version; ./mc --version; \
    curl -fSL --retry 3 -o MINIO-LICENSE https://raw.githubusercontent.com/minio/minio/RELEASE.2025-09-07T16-13-09Z/LICENSE; \
    curl -fSL --retry 3 -o MC-LICENSE https://raw.githubusercontent.com/minio/mc/RELEASE.2025-08-13T08-35-41Z/LICENSE

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=download /out/minio /out/mc /usr/bin/
COPY --from=download /out/MINIO-LICENSE /out/MC-LICENSE /usr/share/licenses/minio/
LABEL org.opencontainers.image.title="TorchLink offline MinIO" \
      org.opencontainers.image.version="RELEASE.2025-09-07T16-13-09Z" \
      org.opencontainers.image.source="https://github.com/minio/minio/tree/RELEASE.2025-09-07T16-13-09Z" \
      org.opencontainers.image.licenses="AGPL-3.0-only"
EXPOSE 9000 9001
ENTRYPOINT ["minio"]
