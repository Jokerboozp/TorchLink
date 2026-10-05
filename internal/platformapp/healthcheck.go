package platformapp

import (
	"net"
	"net/http"
	"strings"
	"time"
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
