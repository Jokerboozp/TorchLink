FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/iot-platform ./cmd/iot-platform \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gb26875-gateway ./cmd/gb26875-gateway \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gb26875-virtual-device ./cmd/gb26875-virtual-device

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
# Source uploads are compiled locally with CGO disabled and vendored dependencies.
COPY --from=build /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
COPY --from=build /out/iot-platform /app/iot-platform
COPY --from=build /out/gb26875-gateway /app/gb26875-gateway
COPY --from=build /out/gb26875-virtual-device /app/gb26875-virtual-device
VOLUME ["/app/data"]
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/iot-platform"]
