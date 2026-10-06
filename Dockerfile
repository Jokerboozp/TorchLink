FROM golang:1.26-alpine@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# Only the Go sources the binaries are built from: package directories plus
# the files they embed (alert rules, tool account scripts). Front end,
# documentation, scripts and protocol examples stay out of the build.
COPY cmd ./cmd
COPY internal ./internal
COPY ops/prometheus ./ops/prometheus
COPY deploy/toolaccounts ./deploy/toolaccounts
# Cluster tools ship in the same image so a deployment needs no Go toolchain:
# cluster-render and cluster-ssh (controller), cluster-init and
# clickhouse-migrate (on a node); capacity-test serves the capacity module.
# Release builds pass the bundle version and commit; local builds report "dev".
ARG IOT_VERSION=dev
ARG IOT_REVISION=
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X iot-platform/internal/version.Version=${IOT_VERSION} -X iot-platform/internal/version.Revision=${IOT_REVISION}" -o /out/ ./cmd/iot-platform ./cmd/iot-access-gateway ./cmd/cluster-render ./cmd/cluster-init ./cmd/clickhouse-migrate ./cmd/cluster-ssh ./cmd/capacity-test
RUN mkdir -p /runtime-data /runtime-run && chmod 0750 /runtime-data && chmod 0700 /runtime-run

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
WORKDIR /app
# Source uploads are compiled locally with CGO disabled and vendored dependencies.
COPY --from=build /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
COPY --from=build /out/ /app/
# Docker initializes a fresh named volume from this directory's ownership.
# Existing volumes retain their ownership and may need the documented repair.
COPY --from=build --chown=65532:65532 /runtime-data/ /app/data/
# Socket directory shared with the protocol runner; a new named volume takes
# its ownership from here.
COPY --from=build --chown=65532:65532 /runtime-run/ /run/torchlink/
VOLUME ["/app/data"]
EXPOSE 8080 26875/tcp 26875/udp
USER nonroot:nonroot
ENTRYPOINT ["/app/iot-platform"]
