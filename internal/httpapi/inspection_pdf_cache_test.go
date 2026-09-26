package httpapi

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/model"
)

// Repeated downloads of one report render it once; when every render slot is
// taken the caller gets errPDFBusy instead of queueing without bound.
func TestInspectionPDFCacheRendersOnceAndBoundsConcurrency(t *testing.T) {
	c := newInspectionPDFCache()
	var renders atomic.Int32
	release := make(chan struct{})
	c.renderPDF = func(report model.DeviceHealthReport) ([]byte, error) {
		renders.Add(1)
		if report.GeneratedAt == 2 {
			<-release
		}
		return []byte("pdf"), nil
	}
	report := model.DeviceHealthReport{GeneratedAt: 1}
	for i := 0; i < 3; i++ {
		if _, err := c.render(context.Background(), "tenant", report); err != nil {
			t.Fatal(err)
		}
	}
	if renders.Load() != 1 {
		t.Fatalf("the same report must render once, rendered %d times", renders.Load())
	}
	for i := 0; i < pdfRenderSlots; i++ {
		go c.render(context.Background(), "tenant-"+string(rune('a'+i)), model.DeviceHealthReport{GeneratedAt: 2})
	}
	for len(c.slots) < pdfRenderSlots {
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.render(ctx, "tenant-z", model.DeviceHealthReport{GeneratedAt: 3}); !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, errPDFBusy) {
		t.Fatalf("a full renderer must not start another render, got %v", err)
	}
	close(release)
}
