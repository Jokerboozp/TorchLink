package logctx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"iot-platform/internal/auth"
)

func TestRecordsCarryRequestIdentity(t *testing.T) {
	var out bytes.Buffer
	log := slog.New(NewHandler(slog.NewJSONHandler(&out, nil)))
	ctx := auth.ContextWithClaims(WithRequestID(context.Background(), "req-7"), auth.Claims{TenantID: "tenant-a", Username: "alice"})
	log.ErrorContext(ctx, "save failed", "error", "conflict")
	for _, want := range []string{`"requestId":"req-7"`, `"tenantId":"tenant-a"`, `"user":"alice"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s in %s", want, out.String())
		}
	}
	out.Reset()
	log.With("component", "jobs").Info("no request")
	if strings.Contains(out.String(), "requestId") || !strings.Contains(out.String(), `"component":"jobs"`) {
		t.Fatalf("records without a request must stay unchanged: %s", out.String())
	}
}
