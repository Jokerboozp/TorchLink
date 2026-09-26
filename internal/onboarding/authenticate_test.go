package onboarding

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type credentialOutageRepository struct{ ports.Repository }

func (credentialOutageRepository) GetManagedDeviceByAccessKey(context.Context, string) (model.ManagedDevice, error) {
	return model.ManagedDevice{}, errors.New("failed to connect: too many clients already")
}

// Only a missing or mismatched credential is an authentication failure; a
// repository failure must let the caller answer "retry later".
func TestAuthenticateSeparatesOutageFromInvalidCredential(t *testing.T) {
	if _, err := New(credentialOutageRepository{memory.NewRepository()}, nil, "", nil).Authenticate(context.Background(), "dk_1", "secret"); !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrAuth) {
		t.Fatalf("outage must be ErrUnavailable, got %v", err)
	}
	if _, err := New(memory.NewRepository(), nil, "", nil).Authenticate(context.Background(), "dk_missing", "secret"); !errors.Is(err, ErrAuth) {
		t.Fatalf("unknown key must be ErrAuth, got %v", err)
	}
}

// A full rate table admits new devices once earlier windows end, instead of
// rejecting every device beyond the limit for a minute.
func TestRateTableReleasesEndedWindows(t *testing.T) {
	s := New(memory.NewRepository(), nil, "", nil)
	for i := 0; i < rateTableLimit; i++ {
		if !s.Allow(fmt.Sprintf("tenant\x00device-%d", i)) {
			t.Fatalf("device %d rejected before the table was full", i)
		}
	}
	if s.Allow("tenant\x00late-device") {
		t.Fatal("a full table must reject a new key within the same second")
	}
	time.Sleep(1100 * time.Millisecond)
	if !s.Allow("tenant\x00late-device") {
		t.Fatal("a new device must be admitted once earlier windows ended")
	}
}
