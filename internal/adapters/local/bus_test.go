package local

import (
	"context"
	"errors"
	"testing"
	"time"
)

// An asynchronous topic returns from Publish before its slow handler finishes,
// drops messages once its queue is full and drains the queue on Close.
func TestAsyncTopicDoesNotBlockPublisher(t *testing.T) {
	bus := NewBus()
	bus.SetAsyncTopic("slow", 1, 2)
	release := make(chan struct{})
	handled := make(chan string, 8)
	if err := bus.Subscribe(context.Background(), "slow", "ai", func(_ context.Context, payload []byte) error {
		<-release
		handled <- string(payload)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	// One message occupies the worker, two fill the queue, the fourth is dropped.
	var dropped int
	for _, payload := range []string{"a", "b", "c", "d"} {
		if err := bus.Publish(context.Background(), "slow", payload, []byte(payload)); errors.Is(err, ErrAsyncQueueFull) {
			dropped++
		} else if err != nil {
			t.Fatal(err)
		}
		if payload == "a" {
			time.Sleep(20 * time.Millisecond) // let the worker take the first message
		}
	}
	if time.Since(started) > time.Second || dropped != 1 {
		t.Fatalf("publisher blocked or queue unbounded: elapsed=%s dropped=%d", time.Since(started), dropped)
	}
	close(release)
	if err := bus.Close(); err != nil {
		t.Fatal(err)
	}
	if len(handled) != 3 {
		t.Fatalf("queued messages must be handled before Close returns, got %d", len(handled))
	}
	if err := bus.Close(); err != nil {
		t.Fatal("closing twice must be safe")
	}
	if err := bus.Publish(context.Background(), "slow", "e", []byte("e")); err == nil {
		t.Fatal("a closed bus must reject messages")
	}
}
