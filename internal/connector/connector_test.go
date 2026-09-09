package connector

import (
	"context"
	"errors"
	"testing"
)

func TestCapabilitiesAndUnsupported(t *testing.T) {
	if len(Types()) != 10 || !Describe(ModbusTCP).Capabilities.ReadOnce || Describe(HTTP).Capabilities.Command || Describe(Edge).Supported {
		t.Fatal("incorrect capabilities")
	}
	a := Adapter{Kind: Edge, Probe: func(context.Context, Request) (*Result, error) { t.Fatal("unsupported probe called"); return nil, nil }}
	if r, e := a.Test(context.Background(), Request{}); !errors.Is(e, ErrUnsupported) || r.ErrorCode != "UNSUPPORTED" {
		t.Fatal(r, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := a.Test(ctx, Request{}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
