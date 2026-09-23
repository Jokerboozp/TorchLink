# 执行当前脚本步骤。
FROM golang:1.26-alpine AS build
# 执行当前脚本步骤。
WORKDIR /src
# 执行当前脚本步骤。
COPY go.mod go.sum ./
# 执行当前脚本步骤。
RUN go mod download
# 执行当前脚本步骤。
COPY . .
# 执行当前脚本步骤。
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/iot-platform ./cmd/iot-access-gateway
# 执行当前脚本步骤。
RUN mkdir -p /runtime-data && chmod 0750 /runtime-data

# 执行当前脚本步骤。
FROM gcr.io/distroless/static-debian12:nonroot
# 执行当前脚本步骤。
WORKDIR /app
# Source uploads are compiled locally with CGO disabled and vendored dependencies.
# 执行当前脚本步骤。
COPY --from=build /usr/local/go /usr/local/go
# 执行当前脚本步骤。
ENV PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
# 执行当前脚本步骤。
COPY --from=build /out/ /app/
# Docker initializes a fresh named volume from this directory's ownership.
# Existing volumes retain their ownership and may need the documented repair.
# 执行当前脚本步骤。
COPY --from=build --chown=65532:65532 /runtime-data/ /app/data/
# 执行当前脚本步骤。
VOLUME ["/app/data"]
# 执行当前脚本步骤。
EXPOSE 8080 26875/tcp 26875/udp
# 执行当前脚本步骤。
USER nonroot:nonroot
# 执行当前脚本步骤。
ENTRYPOINT ["/app/iot-platform"]
