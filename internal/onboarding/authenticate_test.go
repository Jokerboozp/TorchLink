package onboarding

import (
	"context"
	"errors"
	"testing"

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
