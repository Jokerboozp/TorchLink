package opscenter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// overviewPrometheus counts target listings and can block them to simulate a
// slow Prometheus.
type overviewPrometheus struct {
	ports.MetricsBackend
	targetCalls atomic.Int32
	release     chan struct{}
}

func (f *overviewPrometheus) Configured() bool { return true }
func (f *overviewPrometheus) Targets(ctx context.Context) ([]model.ScrapeTarget, error) {
	f.targetCalls.Add(1)
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []model.ScrapeTarget{{Job: "iot-platform", Health: "up"}, {Job: "node", Health: "down", LastError: "connection refused"}}, nil
}
func (f *overviewPrometheus) Query(context.Context, ports.MetricQuery) (model.MetricQueryResult, error) {
	v := 2.0
	return model.MetricQueryResult{Series: []model.MetricSeries{{Values: []*float64{&v}}}}, nil
}

func TestOverviewKPIsReturnsOnlyTheRequestedGroup(t *testing.T) {
	s := &Service{Metrics: &overviewPrometheus{}}
	group, err := s.OverviewKPIs(context.Background(), "host")
	if err != nil {
		t.Fatal(err)
	}
	if len(group.KPIs) != 3 {
		t.Fatalf("host group returned %d KPIs, want 3", len(group.KPIs))
	}
	for _, k := range group.KPIs {
		if k.Group != "host" || k.Status != "scrape_failed" {
			t.Fatalf("unexpected KPI %s group=%s status=%s", k.ID, k.Group, k.Status)
		}
	}
	if job := group.Jobs["node"]; job.Total != 1 || job.Up != 0 || job.LastError == "" {
		t.Fatalf("job health not reported: %+v", group.Jobs)
	}
	var validation *ValidationError
	if _, err := s.OverviewKPIs(context.Background(), "unknown"); !errors.As(err, &validation) {
		t.Fatalf("unknown group should be a validation error, got %v", err)
	}
}

// The overview page loads every KPI group in parallel; they must share one
// Prometheus target listing instead of issuing one each.
func TestOverviewKPIGroupsShareOneTargetListing(t *testing.T) {
	prom := &overviewPrometheus{release: make(chan struct{})}
	s := &Service{Metrics: prom}
	var wg sync.WaitGroup
	for _, group := range []string{"platform", "backup", "host", "observability"} {
		wg.Add(1)
		go func(group string) {
			defer wg.Done()
			if _, err := s.OverviewKPIs(context.Background(), group); err != nil {
				t.Error(err)
			}
		}(group)
	}
	time.Sleep(50 * time.Millisecond)
	close(prom.release)
	wg.Wait()
	if _, err := s.OverviewKPIs(context.Background(), "platform"); err != nil {
		t.Fatal(err)
	}
	if calls := prom.targetCalls.Load(); calls != 1 {
		t.Fatalf("target listing called %d times, want 1", calls)
	}
}

// A caller that gives up must not cancel the shared listing for others.
func TestOverviewTargetsSurviveFirstCallerCancel(t *testing.T) {
	prom := &overviewPrometheus{release: make(chan struct{})}
	s := &Service{Metrics: prom}
	first, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { _, err := s.overviewTargets(first); firstDone <- err }()
	time.Sleep(20 * time.Millisecond)
	secondDone := make(chan error, 1)
	go func() { _, err := s.overviewTargets(context.Background()); secondDone <- err }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first caller: %v", err)
	}
	close(prom.release)
	if err := <-secondDone; err != nil {
		t.Fatalf("second caller should get the shared result: %v", err)
	}
}

func TestComponentRejectsUnknownID(t *testing.T) {
	s := &Service{}
	if status, ok := s.Component(context.Background(), "loki"); !ok || status.State != "unconfigured" {
		t.Fatalf("loki without backend: ok=%v status=%+v", ok, status)
	}
	if _, ok := s.Component(context.Background(), "../metrics"); ok {
		t.Fatal("unknown component accepted")
	}
}
