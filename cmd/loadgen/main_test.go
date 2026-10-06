package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHistogramPercentilesUseBucketBounds(t *testing.T) {
	h := newHistogram()
	if h.percentile(0.95) != 0 {
		t.Fatal("empty histogram has a percentile")
	}
	for i := 0; i < 90; i++ {
		h.observe(3 * time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		h.observe(90 * time.Millisecond)
	}
	if p50, p95 := h.percentile(0.5), h.percentile(0.95); p50 != 5 || p95 != 100 {
		t.Fatalf("p50=%v p95=%v", p50, p95)
	}
	h.observe(2 * time.Minute)
	if p100 := h.percentile(1); p100 != 120000 {
		t.Fatalf("values beyond the last bound report the maximum: %v", p100)
	}
}

func TestCurveTargets(t *testing.T) {
	total := 100 * time.Second
	if got := curveTarget("steady", 10*time.Second, total, 50, 0, 0, 0); got != 500 {
		t.Fatalf("steady %d", got)
	}
	// Burst starts at a quarter of the run and lasts burstWindow.
	if got := curveTarget("burst", 25*time.Second, total, 10, 100, 5*time.Second, 0); got != 250 {
		t.Fatalf("burst before %d", got)
	}
	if got := curveTarget("burst", 30*time.Second, total, 10, 100, 5*time.Second, 0); got != 750 {
		t.Fatalf("burst end %d", got)
	}
	if got := curveTarget("burst", 40*time.Second, total, 10, 100, 5*time.Second, 0); got != 850 {
		t.Fatalf("burst after %d", got)
	}
	// Offline sends nothing, then catches up at the burst rate up to what was generated.
	if got := curveTarget("offline", 10*time.Second, total, 10, 100, 0, 20*time.Second); got != 0 {
		t.Fatalf("offline %d", got)
	}
	if got := curveTarget("offline", 21*time.Second, total, 10, 100, 0, 20*time.Second); got != 100 {
		t.Fatalf("recovery %d", got)
	}
	if got := curveTarget("offline", 60*time.Second, total, 10, 100, 0, 20*time.Second); got != 600 {
		t.Fatalf("caught up %d", got)
	}
}

func TestPayloadSpreadsDevices(t *testing.T) {
	var body struct {
		TenantID string `json:"tenantId"`
		DeviceID string `json:"deviceId"`
	}
	if err := json.Unmarshal(payload(1003, 1000, "t", "p"), &body); err != nil || body.DeviceID != "device_000003" || body.TenantID != "t" {
		t.Fatalf("payload %+v %v", body, err)
	}
}
