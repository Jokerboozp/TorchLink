package httpapi

import (
	"context"
	"errors"
	"sync"
	"time"

	"iot-platform/internal/core"
	"iot-platform/internal/model"
)

// Rendering holds a whole PDF in memory, so only a few run at once; a caller
// waits up to pdfRenderWait for a slot. The last PDF of each tenant is kept
// until its report changes, so repeated downloads do not render again.
const (
	pdfRenderSlots = 2
	pdfRenderWait  = 30 * time.Second
)

var errPDFBusy = errors.New("inspection PDF rendering is busy")

type cachedInspectionPDF struct {
	generatedAt int64
	data        []byte
}

type inspectionPDFCache struct {
	slots     chan struct{}
	mu        sync.Mutex
	latest    map[string]cachedInspectionPDF
	renderPDF func(model.DeviceHealthReport) ([]byte, error)
}

func newInspectionPDFCache() *inspectionPDFCache {
	return &inspectionPDFCache{slots: make(chan struct{}, pdfRenderSlots), latest: map[string]cachedInspectionPDF{}, renderPDF: core.RenderHealthInspectionPDF}
}

func (c *inspectionPDFCache) cached(tenantID string, generatedAt int64) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.latest[tenantID]
	return entry.data, ok && entry.generatedAt == generatedAt
}

// render returns the PDF of report, from the cache when that report was
// already rendered, and errPDFBusy when no render slot frees up in time.
func (c *inspectionPDFCache) render(ctx context.Context, tenantID string, report model.DeviceHealthReport) ([]byte, error) {
	if c == nil {
		return core.RenderHealthInspectionPDF(report)
	}
	if data, ok := c.cached(tenantID, report.GeneratedAt); ok {
		return data, nil
	}
	timer := time.NewTimer(pdfRenderWait)
	defer timer.Stop()
	select {
	case c.slots <- struct{}{}:
	case <-timer.C:
		return nil, errPDFBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.slots }()
	// Another request may have rendered the same report while this one waited.
	if data, ok := c.cached(tenantID, report.GeneratedAt); ok {
		return data, nil
	}
	data, err := c.renderPDF(report)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.latest[tenantID] = cachedInspectionPDF{generatedAt: report.GeneratedAt, data: data}
	c.mu.Unlock()
	return data, nil
}
