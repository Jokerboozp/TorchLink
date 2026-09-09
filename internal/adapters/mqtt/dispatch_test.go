package mqttadapter

import (
	"io"
	"log/slog"
	"testing"
)

func TestBoundedIngressRejectsOverload(t *testing.T) {
	c := &Client{jobs: make(chan func(), 2), stop: make(chan struct{}), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ran := 0
	for i := 0; i < 2; i++ {
		if !c.enqueue("test", func() { ran++ }) {
			t.Fatal("early rejection")
		}
	}
	if c.enqueue("test", func() { t.Fatal("overload executed") }) {
		t.Fatal("unbounded queue")
	}
	if ran != 0 || len(c.jobs) != 2 {
		t.Fatal("callback executed inline or queue changed")
	}
	(<-c.jobs)()
	(<-c.jobs)()
	if ran != 2 {
		t.Fatal("accepted jobs lost")
	}
	close(c.stop)
	if c.enqueue("test", func() {}) {
		t.Fatal("enqueue after shutdown")
	}
}
