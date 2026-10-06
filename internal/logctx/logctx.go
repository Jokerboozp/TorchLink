// Package logctx carries request identity into log records: every record
// logged with a request context gets the request ID and, once authenticated,
// the tenant and user, so one request's records can be found together.
package logctx

import (
	"context"
	"log/slog"

	"iot-platform/internal/auth"
)

type requestIDKey struct{}

// WithRequestID returns ctx carrying the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request ID carried by ctx, if any.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// Handler adds requestId, tenantId and user from the record's context.
// Records logged without a context (slog.Info rather than InfoContext) are
// passed through unchanged.
type Handler struct{ slog.Handler }

// NewHandler wraps next.
func NewHandler(next slog.Handler) *Handler { return &Handler{Handler: next} }

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		if id := RequestID(ctx); id != "" {
			r.AddAttrs(slog.String("requestId", id))
		}
		if claims, ok := auth.ClaimsFromContext(ctx); ok {
			if claims.TenantID != "" {
				r.AddAttrs(slog.String("tenantId", claims.TenantID))
			}
			if claims.Username != "" {
				r.AddAttrs(slog.String("user", claims.Username))
			}
		}
	}
	return h.Handler.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{Handler: h.Handler.WithGroup(name)}
}
