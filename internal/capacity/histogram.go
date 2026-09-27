package capacity

import (
	"math"
	"sort"
	"sync"
)

// Histogram buckets are log-spaced (10% wide) from 0.1 ms, so every
// instance and agent uses identical boundaries and percentiles are computed on
// merged counts instead of averaging per-node percentiles.
const (
	histBase   = 0.1
	histGrowth = 1.1
	histMaxIdx = 200 // ~0.1ms * 1.1^200 ≈ 19 hours
)

type Histogram struct {
	Counts map[int]uint64 `json:"counts"`
	N      uint64         `json:"n"`
	SumMS  float64        `json:"sumMs"`
	MaxMS  float64        `json:"maxMs"`
}

func bucketOf(ms float64) int {
	if ms <= histBase {
		return 0
	}
	i := int(math.Ceil(math.Log(ms/histBase) / math.Log(histGrowth)))
	return min(max(i, 0), histMaxIdx)
}

func bucketUpper(i int) float64 { return histBase * math.Pow(histGrowth, float64(i)) }

func (h *Histogram) Observe(ms float64) {
	if math.IsNaN(ms) {
		return
	}
	if ms < 0 {
		ms = 0
	}
	if h.Counts == nil {
		h.Counts = map[int]uint64{}
	}
	h.Counts[bucketOf(ms)]++
	h.N++
	h.SumMS += ms
	h.MaxMS = math.Max(h.MaxMS, ms)
}

func (h *Histogram) Merge(o Histogram) {
	if o.N == 0 {
		return
	}
	if h.Counts == nil {
		h.Counts = map[int]uint64{}
	}
	for k, v := range o.Counts {
		h.Counts[k] += v
	}
	h.N += o.N
	h.SumMS += o.SumMS
	h.MaxMS = math.Max(h.MaxMS, o.MaxMS)
}

// Quantile returns the bucket upper bound (at most 10% above the true value),
// capped at the observed maximum. ok is false without samples.
func (h Histogram) Quantile(q float64) (float64, bool) {
	if h.N == 0 {
		return 0, false
	}
	target := uint64(math.Ceil(q * float64(h.N)))
	if target == 0 {
		target = 1
	}
	keys := make([]int, 0, len(h.Counts))
	for k := range h.Counts {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var seen uint64
	for _, k := range keys {
		seen += h.Counts[k]
		if seen >= target {
			return math.Min(bucketUpper(k), h.MaxMS), true
		}
	}
	return h.MaxMS, true
}

// Percentiles is the reported form. A percentile is null when the sample count
// cannot support it (P95 needs 20 samples, P99 needs 100).
type Percentiles struct {
	N      uint64   `json:"n"`
	MeanMS *float64 `json:"meanMs"`
	P50MS  *float64 `json:"p50Ms"`
	P95MS  *float64 `json:"p95Ms"`
	P99MS  *float64 `json:"p99Ms"`
	MaxMS  *float64 `json:"maxMs"`
}

func (h Histogram) Percentiles() Percentiles {
	p := Percentiles{N: h.N}
	if h.N == 0 {
		return p
	}
	round := func(v float64) *float64 {
		if v >= 100 {
			v = math.Round(v)
		} else {
			v = math.Round(v*10) / 10
		}
		return &v
	}
	p.MeanMS = round(h.SumMS / float64(h.N))
	p.MaxMS = round(h.MaxMS)
	v, _ := h.Quantile(0.5)
	p.P50MS = round(v)
	if h.N >= 20 {
		v, _ = h.Quantile(0.95)
		p.P95MS = round(v)
	}
	if h.N >= 100 {
		v, _ = h.Quantile(0.99)
		p.P99MS = round(v)
	}
	return p
}

// StreamStats aggregates one load stream (http, mqtt, tcp, query) for the
// measurement window. Attempts count retries; Sent counts unique messages.
type StreamStats struct {
	Scheduled   uint64            `json:"scheduled"`
	Sent        uint64            `json:"sent"`
	NotSent     uint64            `json:"notSent"`
	OK          uint64            `json:"ok"`
	Fail        uint64            `json:"fail"`
	Attempts    uint64            `json:"attempts"`
	Bytes       uint64            `json:"bytes"`
	Codes       map[string]uint64 `json:"codes"`
	Latency     Histogram         `json:"latency"`
	FailLatency Histogram         `json:"failLatency"`
	Lateness    Histogram         `json:"lateness"`
	WarmupSent  uint64            `json:"warmupSent"`
}

func (s *StreamStats) Merge(o *StreamStats) {
	if o == nil {
		return
	}
	s.Scheduled += o.Scheduled
	s.Sent += o.Sent
	s.NotSent += o.NotSent
	s.OK += o.OK
	s.Fail += o.Fail
	s.Attempts += o.Attempts
	s.Bytes += o.Bytes
	s.WarmupSent += o.WarmupSent
	if s.Codes == nil {
		s.Codes = map[string]uint64{}
	}
	for k, v := range o.Codes {
		s.Codes[k] += v
	}
	s.Latency.Merge(o.Latency)
	s.FailLatency.Merge(o.FailLatency)
	s.Lateness.Merge(o.Lateness)
}

// streamRecorder is the concurrent-safe builder used by agents.
type streamRecorder struct {
	mu sync.Mutex
	s  StreamStats
}

func (r *streamRecorder) scheduled(measured bool) {
	if !measured {
		return
	}
	r.mu.Lock()
	r.s.Scheduled++
	r.mu.Unlock()
}

func (r *streamRecorder) notSent(measured bool) {
	if !measured {
		return
	}
	r.mu.Lock()
	r.s.NotSent++
	r.code("not_sent")
	r.mu.Unlock()
}

func (r *streamRecorder) done(measured, ok bool, code string, latencyMS, latenessMS float64, attempts int, bytes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !measured {
		r.s.WarmupSent++
		return
	}
	r.s.Sent++
	r.s.Attempts += uint64(max(attempts, 1))
	r.s.Bytes += uint64(max(bytes, 0))
	r.code(code)
	r.s.Lateness.Observe(latenessMS)
	if ok {
		r.s.OK++
		r.s.Latency.Observe(latencyMS)
	} else {
		r.s.Fail++
		r.s.FailLatency.Observe(latencyMS)
	}
}

func (r *streamRecorder) code(c string) {
	if r.s.Codes == nil {
		r.s.Codes = map[string]uint64{}
	}
	r.s.Codes[c]++
}

func (r *streamRecorder) snapshot() *StreamStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := StreamStats{}
	out.Merge(&r.s)
	return &out
}
