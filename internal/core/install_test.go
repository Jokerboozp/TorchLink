package core

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/parser"
)

func TestInstallSetsDependenciesOnlyBeforeStart(t *testing.T) {
	engine := New(memory.NewRepository(), nil, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), nil)
	raw := engine.RawStore
	if err := engine.Install(Deps{PublishExternalTopics: true, BusinessRunTimeout: 3}); err != nil {
		t.Fatal(err)
	}
	if !engine.PublishExternalTopics || engine.BusinessRunTimeout != 3 || engine.RawStore != raw {
		t.Fatalf("Install did not apply the dependencies: %+v", engine)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := engine.StartWith(ctx, Components{}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Install(Deps{}); !errors.Is(err, errInstalledLate) {
		t.Fatalf("Install after start = %v, want errInstalledLate", err)
	}
}
