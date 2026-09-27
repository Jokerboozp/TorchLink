package capacity

import (
	"context"
	"math"
	"time"
)

// StepRunner executes one fully settled load step (offer, drain, verify,
// judge). hold is the measurement window for that step.
type StepRunner interface {
	RunStep(ctx context.Context, rate float64, kind string, hold time.Duration) (PhaseRecord, error)
	// Affordable reports whether a step with this window still fits the
	// wall-time budget.
	Affordable(hold time.Duration) bool
}

// SearchResult is the capacity conclusion for mixed business messages.
type SearchResult struct {
	Preset                    string   `json:"preset"`
	Classification            string   `json:"classification"`
	LowerPassedBound          *float64 `json:"lowerPassedBound"`
	UpperFailedBound          *float64 `json:"upperFailedBound"`
	RecommendedOperatingValue *float64 `json:"recommendedOperatingValue"`
	RecommendationBasis       string   `json:"recommendationBasis,omitempty"`
	RelativeWidth             *float64 `json:"relativeWidth"`
	HealthyVerifiedSeconds    float64  `json:"healthyVerifiedSeconds"`
	StopReason                string   `json:"stopReason,omitempty"`
	FailureMode               string   `json:"failureMode,omitempty"`
	Unstable                  bool     `json:"unstable"`
	NonMonotonic              bool     `json:"nonMonotonic"`
	Steps                     int      `json:"steps"`
}

// Classifications of a search.
const (
	ClassBounded      = "bounded"          // passed L, failed U, within the target width
	ClassWide         = "bounded_wide"     // passed L, failed U, stopped before the target width
	ClassLowerOnly    = "lower_bound_only" // passed L, no failing step found
	ClassNoPass       = "no_pass"          // the first step already failed
	ClassInconclusive = "inconclusive"
	ClassRegression   = "regression" // quick preset: fixed steps, no capacity claim
	ClassSoak         = "soak"
	ClassResilience   = "resilience"
	ClassUnmeasured   = "unmeasured"
)

// RecommendationFactor is an operating policy, not a measurement; without a
// fault-capacity result the value is labelled healthy_only.
const RecommendationFactor = 0.7

type searcher struct {
	p       *Plan
	run     StepRunner
	res     SearchResult
	passes  []float64
	fails   []float64
	maxRate float64
}

// Search runs the preset's step sequence. Errors are controller failures;
// capacity outcomes are always expressed in the result.
func Search(ctx context.Context, p *Plan, run StepRunner) (SearchResult, error) {
	s := &searcher{p: p, run: run, res: SearchResult{Preset: p.Preset, Classification: ClassUnmeasured}}
	s.maxRate = math.Min(p.Budget.MaximumMessagesPerSecond, p.MaxRateForDevices())
	var err error
	switch p.Preset {
	case PresetQuick:
		err = s.fixed(ctx, "quick", p.Search.Measure.D(), ClassRegression)
	case PresetSoak:
		err = s.fixed(ctx, "soak", p.Search.CandidateHold.D(), ClassSoak)
	case PresetResilience:
		err = s.fixed(ctx, PresetResilience, p.Search.Measure.D(), ClassResilience)
	default:
		err = s.capacity(ctx)
	}
	s.finish()
	return s.res, err
}

// step runs one rate and records the outcome; ok=false means stop searching.
func (s *searcher) step(ctx context.Context, rate float64, kind string, hold time.Duration) (string, bool, error) {
	if ctx.Err() != nil {
		s.res.StopReason = ReasonCancel
		return "", false, nil
	}
	if s.res.Steps >= s.p.Search.MaxSteps || !s.run.Affordable(hold) {
		s.res.StopReason = ReasonBudget
		return "", false, nil
	}
	rec, err := s.run.RunStep(ctx, rate, kind, hold)
	s.res.Steps++
	if err != nil {
		s.res.StopReason = ReasonInfrastructure
		return "", false, err
	}
	switch rec.Verdict {
	case VerdictPassed:
		s.passes = append(s.passes, rate)
		s.res.HealthyVerifiedSeconds += rec.MeasureSeconds
	case VerdictFailed:
		s.fails = append(s.fails, rate)
		if s.res.FailureMode == "" || rate <= minOf(s.fails) {
			s.res.FailureMode = rec.StopReason
		}
	default:
		s.res.StopReason = rec.StopReason
		if rec.Cancelled {
			s.res.StopReason = ReasonCancel
		}
		return rec.Verdict, false, nil
	}
	return rec.Verdict, true, nil
}

func (s *searcher) fixed(ctx context.Context, kind string, hold time.Duration, class string) error {
	rates := s.p.Search.Rates
	if len(rates) == 0 {
		rates = []float64{s.p.Load.InitialMessagesPerSecond}
	}
	s.res.Classification = class
	for _, r := range rates {
		_, cont, err := s.step(ctx, r, kind, hold)
		if err != nil || !cont {
			return err
		}
	}
	return nil
}

func (s *searcher) capacity(ctx context.Context) error {
	sp := s.p.Search
	rate := math.Min(roundRate(s.p.Load.InitialMessagesPerSecond), s.maxRate)
	// Coarse ramp until the first failure.
	for {
		verdict, cont, err := s.step(ctx, rate, "ramp", sp.Measure.D())
		if err != nil || !cont {
			return err
		}
		if verdict == VerdictFailed {
			break
		}
		next := math.Min(roundRate(rate*sp.RampFactor), s.maxRate)
		if next <= rate {
			// The budget or per-device allowance caps the load: at least rate.
			s.res.StopReason = ReasonBudget
			if s.maxRate < s.p.Budget.MaximumMessagesPerSecond {
				s.res.StopReason = ReasonPolicy
			}
			return nil
		}
		rate = next
	}
	if len(s.passes) == 0 {
		return nil
	}
	// Refine between the highest pass and the lowest failure.
	for {
		lo, hi := maxOf(s.passes), minOf(s.fails)
		if hi-lo <= sp.BoundaryRelativeWidth*math.Max(lo, 1) {
			break
		}
		mid := roundRate((lo + hi) / 2)
		if mid <= lo || mid >= hi {
			break
		}
		if _, cont, err := s.step(ctx, mid, "refine", sp.Measure.D()); err != nil || !cont {
			return err
		}
	}
	// Confirm the candidate with longer repeated holds.
	candidate := maxOf(s.passes)
	for i := 0; i < sp.Repeats; i++ {
		verdict, cont, err := s.step(ctx, candidate, "confirm", sp.CandidateHold.D())
		if err != nil || !cont {
			return err
		}
		if verdict == VerdictFailed {
			s.res.Unstable = true
			return nil
		}
	}
	return nil
}

func (s *searcher) finish() {
	r := &s.res
	for _, f := range s.fails {
		for _, p := range s.passes {
			if p > f {
				r.NonMonotonic = true
			}
		}
	}
	if r.Classification == ClassRegression || r.Classification == ClassSoak || r.Classification == ClassResilience {
		if len(s.passes) > 0 {
			v := maxOf(s.passes)
			r.LowerPassedBound = &v
		}
		if len(s.fails) > 0 {
			v := minOf(s.fails)
			r.UpperFailedBound = &v
		}
		return
	}
	switch {
	case len(s.passes) == 0 && len(s.fails) > 0:
		r.Classification = ClassNoPass
		v := minOf(s.fails)
		r.UpperFailedBound = &v
		return
	case len(s.passes) == 0:
		r.Classification = ClassInconclusive
		return
	}
	lower := maxOf(s.passes)
	// A failure at or below a pass makes the highest pass unreliable: report
	// the highest pass strictly below every failure.
	if len(s.fails) > 0 {
		hi := minOf(s.fails)
		lower = math.Inf(-1)
		for _, p := range s.passes {
			if p < hi && p > lower {
				lower = p
			}
		}
		if math.IsInf(lower, -1) {
			r.Classification = ClassNoPass
			r.UpperFailedBound = &hi
			return
		}
		r.UpperFailedBound = &hi
		width := (hi - lower) / math.Max(lower, 1)
		r.RelativeWidth = &width
		r.Classification = ClassWide
		if width <= s.p.Search.BoundaryRelativeWidth {
			r.Classification = ClassBounded
		}
	} else {
		r.Classification = ClassLowerOnly
	}
	r.LowerPassedBound = &lower
	if r.Unstable || r.NonMonotonic {
		return
	}
	confirmed := 0
	for _, p := range s.passes {
		if p == lower {
			confirmed++
		}
	}
	if confirmed > s.p.Search.Repeats {
		v := math.Floor(lower * RecommendationFactor)
		r.RecommendedOperatingValue, r.RecommendationBasis = &v, "healthy_only"
	}
}

// roundRate keeps rates readable: integers above 10, one decimal below.
func roundRate(r float64) float64 {
	if r >= 10 {
		return math.Round(r)
	}
	return math.Max(0.1, math.Round(r*10)/10)
}

func maxOf(v []float64) float64 {
	m := math.Inf(-1)
	for _, x := range v {
		m = math.Max(m, x)
	}
	return m
}

func minOf(v []float64) float64 {
	m := math.Inf(1)
	for _, x := range v {
		m = math.Min(m, x)
	}
	return m
}
