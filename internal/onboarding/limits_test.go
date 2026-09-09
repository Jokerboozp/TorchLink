package onboarding

import (
	"context"
	"iot-platform/internal/connector"
	"testing"
)

func TestProbeLimitAndCancellation(t *testing.T) {
	s, _, q := fixture(t)
	for i := 0; i < cap(testSlots); i++ {
		testSlots <- struct{}{}
	}
	r, e := s.Test(context.Background(), "tenant", q)
	for i := 0; i < cap(testSlots); i++ {
		<-testSlots
	}
	if e != nil || r.Success || r.ErrorCode != "BUSY" {
		t.Fatal(r, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, e = s.Test(ctx, "tenant", q)
	if e == nil || r.ErrorCode != "CANCELED" {
		t.Fatal(r, e)
	}
	q.Type = connector.Edge
	r, e = s.Test(context.Background(), "tenant", q)
	if e == nil || r.ErrorCode != "UNSUPPORTED" {
		t.Fatal(r, e)
	}
}
