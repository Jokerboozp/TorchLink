package connector

import (
	"context"
	"errors"
	"net"
)

var ErrAuthentication = errors.New("connector authentication failed")

// ErrorCode uses typed errors; arbitrary broker/driver text is never interpreted
// as proof of an authentication failure.
func ErrorCode(err error, fallback string) string {
	if errors.Is(err, ErrAuthentication) {
		return "AUTH_FAILED"
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		return "TIMEOUT"
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	return fallback
}
