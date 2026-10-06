package platformapp

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/protocolrunner"
)

// healthcheck backs `iot-platform healthcheck`, the container health probe:
// the distroless image has no shell or HTTP client. It asks this process's
// own /health/live and returns the exit code; it needs no other configuration.
func healthcheck(addr string) int {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		host, port = "", "8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + net.JoinHostPort(host, port) + "/health/live")
	if err != nil {
		return 1
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// runnerHealthcheck backs `iot-platform runner-healthcheck`, the probe of
// the protocol-runner container, which has no network: it asks the runner's
// Unix socket for /health.
func runnerHealthcheck(socket string) int {
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if protocolrunner.NewClient(socket).Health(ctx) != nil {
		return 1
	}
	return 0
}
