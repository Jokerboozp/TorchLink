package httpapi

import (
	"context"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/onboarding"
)

// Without broker revocation a standard device token must stay short because
// expiry is the only way to cut off a revoked credential; with revocation it
// can last long enough that devices are not reconnected every five minutes.
func TestStandardDeviceTokenTTLFollowsRevocation(t *testing.T) {
	s := &Server{cfg: config.Config{MQTTDeviceTokenTTL: 12 * time.Hour}, onboarding: onboarding.New(memory.NewRepository(), nil, "", nil)}
	if got := s.standardDeviceTokenTTL(); got != 5*time.Minute {
		t.Fatalf("without revocation: %s", got)
	}
	s.onboarding.RevokeUsername = func(context.Context, string) error { return nil }
	if got := s.standardDeviceTokenTTL(); got != 12*time.Hour {
		t.Fatalf("with revocation: %s", got)
	}
}
