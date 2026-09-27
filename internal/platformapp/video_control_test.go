package platformapp

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
)

func TestVideoControlRunsOnOneInstanceAndHandsOver(t *testing.T) {
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var runningA, runningB atomic.Int32
	a, b := &videoControl{}, &videoControl{}
	ctxA, stopA := context.WithCancel(context.Background())
	ctxB, stopB := context.WithCancel(context.Background())
	defer stopB()
	starter := func(n *atomic.Int32) func(context.Context) {
		return func(ctx context.Context) {
			n.Add(1)
			go func() { <-ctx.Done(); n.Add(-1) }()
		}
	}
	go a.run(ctxA, repo, "a", "http://a:8080", starter(&runningA), func() {}, log, 20*time.Millisecond)
	waitFor(t, func() bool { local, _ := a.state(); return local })
	go b.run(ctxB, repo, "b", "http://b:8080", starter(&runningB), func() {}, log, 20*time.Millisecond)
	waitFor(t, func() bool { _, endpoint := b.state(); return endpoint == "http://a:8080" })
	if local, _ := b.state(); local || runningB.Load() != 0 || runningA.Load() != 1 {
		t.Fatal("standby started the live module")
	}
	stopA()
	waitFor(t, func() bool { local, _ := b.state(); return local })
	waitFor(t, func() bool { return runningA.Load() == 0 && runningB.Load() == 1 })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
