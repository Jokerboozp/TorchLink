FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/iot-platform ./cmd/iot-access-gateway

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
# Source uploads are compiled locally with CGO disabled and vendored dependencies.
COPY --from=build /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
COPY --from=build /out/ /app/
VOLUME ["/app/data"]
EXPOSE 8080 26875/tcp 26875/udp
USER nonroot:nonroot
ENTRYPOINT ["/app/iot-platform"]
