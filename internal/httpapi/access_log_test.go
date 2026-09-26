package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Routine successful requests (page polling, Prometheus scrapes) must not be
// written at the default info level; failures still are.
func TestAccessLogKeepsRoutineRequestsOutOfInfoLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var out bytes.Buffer
	s := &Server{log: slog.New(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	router := gin.New()
	router.Use(s.accessLog())
	router.GET("/metrics", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/missing", func(c *gin.Context) { c.Status(http.StatusNotFound) })
	router.GET("/broken", func(c *gin.Context) { c.Status(http.StatusBadGateway) })

	for _, path := range []string{"/metrics", "/metrics", "/missing", "/broken"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	logged := out.String()
	if strings.Contains(logged, `"path":"/metrics"`) {
		t.Fatalf("successful request logged at info level: %s", logged)
	}
	if !strings.Contains(logged, `"level":"INFO","msg":"http request","method":"GET","path":"/missing"`) {
		t.Fatalf("client error not logged at info: %s", logged)
	}
	if !strings.Contains(logged, `"level":"WARN","msg":"http request","method":"GET","path":"/broken"`) {
		t.Fatalf("server error not logged at warn: %s", logged)
	}
}
