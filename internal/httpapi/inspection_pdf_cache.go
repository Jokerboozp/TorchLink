package httpapi

import (
	"context"
	"errors"
	"golang.org/x/sync/singleflight"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"sync"
	"time"
)

const (
	pdfRenderSlots  = 2
	pdfCacheBytes   = 64 << 20
	pdfCacheEntries = 16
)

var errPDFBusy = errors.New("inspection PDF rendering is busy")

type cachedInspectionPDF struct {
	id   string
	data []byte
	used time.Time
}
type inspectionPDFCache struct {
	slots     chan struct{}
	mu        sync.Mutex
	latest    map[string]cachedInspectionPDF
	runs      singleflight.Group
	renderPDF func(model.DeviceHealthReport) ([]byte, error)
}

func newInspectionPDFCache() *inspectionPDFCache {
	return &inspectionPDFCache{slots: make(chan struct{}, pdfRenderSlots), latest: map[string]cachedInspectionPDF{}, renderPDF: core.RenderHealthInspectionPDF}
}
func (c *inspectionPDFCache) cached(tenant, id string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.latest[tenant]
	if ok && v.id == id {
		v.used = time.Now()
		c.latest[tenant] = v
		return v.data, true
	}
	return nil, false
}

// Metadata is read first; only a cache miss acquires a render slot and reads a
// bounded detail page. Concurrent downloads of the same immutable ID share work.
func (c *inspectionPDFCache) load(ctx context.Context, tenant, id string, loader func(context.Context) (model.DeviceHealthReport, error)) ([]byte, error) {
	if data, ok := c.cached(tenant, id); ok {
		return data, nil
	}
	result := c.runs.DoChan(tenant+"\x00"+id, func() (any, error) {
		if data, ok := c.cached(tenant, id); ok {
			return data, nil
		}
		select {
		case c.slots <- struct{}{}:
		default:
			return nil, errPDFBusy
		}
		defer func() { <-c.slots }()
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		report, err := loader(loadCtx)
		if err != nil {
			return nil, err
		}
		data, err := c.renderPDF(report)
		if err != nil {
			return nil, err
		}
		if len(data) <= pdfCacheBytes {
			c.mu.Lock()
			delete(c.latest, tenant)
			for {
				size := len(data)
				oldest := ""
				var at time.Time
				for key, v := range c.latest {
					size += len(v.data)
					if oldest == "" || v.used.Before(at) {
						oldest, at = key, v.used
					}
				}
				if size <= pdfCacheBytes && len(c.latest) < pdfCacheEntries {
					break
				}
				delete(c.latest, oldest)
			}
			c.latest[tenant] = cachedInspectionPDF{id: id, data: data, used: time.Now()}
			c.mu.Unlock()
		}
		return data, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case v := <-result:
		if v.Err != nil {
			return nil, v.Err
		}
		return v.Val.([]byte), nil
	}
}
