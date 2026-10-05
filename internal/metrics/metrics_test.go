package metrics

import (
	"strings"
	"testing"
)

func TestLabeledSeriesAndHistogramExposition(t *testing.T) {
	r := New()
	r.Inc(Series("ai_run_total", "workflow", "alarm-handler", "status", "SUCCEEDED"))
	r.Add(Series("ai_run_total", "workflow", `odd"name`, "status", "FAILED"), 2)
	r.Observe(Series("ai_run_duration_seconds", "workflow", "alarm-handler"), 3)
	r.Observe(Series("ai_run_duration_seconds", "workflow", "alarm-handler"), 700)
	out := r.Prometheus()
	if strings.Count(out, "# TYPE ai_run_total counter\n") != 1 || strings.Count(out, "# TYPE ai_run_duration_seconds histogram\n") != 1 {
		t.Fatalf("each metric needs exactly one TYPE line:\n%s", out)
	}
	for _, line := range []string{
		`ai_run_total{workflow="alarm-handler",status="SUCCEEDED"} 1`,
		`ai_run_total{workflow="odd\"name",status="FAILED"} 2`,
		`ai_run_duration_seconds_bucket{workflow="alarm-handler",le="2"} 0`,
		`ai_run_duration_seconds_bucket{workflow="alarm-handler",le="5"} 1`,
		`ai_run_duration_seconds_bucket{workflow="alarm-handler",le="600"} 1`,
		`ai_run_duration_seconds_bucket{workflow="alarm-handler",le="+Inf"} 2`,
		`ai_run_duration_seconds_sum{workflow="alarm-handler"} 703`,
		`ai_run_duration_seconds_count{workflow="alarm-handler"} 2`,
		"parse_success_total 0",
	} {
		if !strings.Contains(out, line+"\n") {
			t.Errorf("missing %q in\n%s", line, out)
		}
	}
}
