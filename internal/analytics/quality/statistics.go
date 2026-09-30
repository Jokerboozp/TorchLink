package quality

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func numeric(v any) (float64, bool) {
	var n float64
	switch v := v.(type) {
	case float64:
		n = v
	case float32:
		n = float64(v)
	case int:
		n = float64(v)
	case int8:
		n = float64(v)
	case int16:
		n = float64(v)
	case int32:
		n = float64(v)
	case int64:
		n = float64(v)
	case uint:
		n = float64(v)
	case uint8:
		n = float64(v)
	case uint16:
		n = float64(v)
	case uint32:
		n = float64(v)
	case uint64:
		n = float64(v)
	case json.Number:
		var err error
		n, err = v.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return n, finite(n)
}

func valueType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "number", "float", "double":
		return "number"
	case "integer", "int", "long":
		return "integer"
	case "boolean", "bool":
		return "boolean"
	case "string", "enum", "state":
		return "string"
	case "object":
		return "object"
	case "array":
		return "array"
	}
	return ""
}

func validValue(p Profile, v any) bool {
	if v == nil {
		return false
	}
	switch valueType(p.ValueType) {
	case "number":
		_, ok := numeric(v)
		return ok
	case "integer":
		n, ok := numeric(v)
		return ok && math.Trunc(n) == n
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok && finiteJSON(v)
	case "array":
		_, ok := v.([]any)
		return ok && finiteJSON(v)
	}
	return false
}

// json.Marshal recursively rejects non-finite nested values as well.
func finiteJSON(v any) bool { _, err := json.Marshal(v); return err == nil }

func ValidateProfile(p Profile) error {
	if p.ID == "" || p.Version == "" || p.EffectiveFrom.IsZero() {
		return fmt.Errorf("profile id, version and effectiveFrom are required")
	}
	if !p.EffectiveTo.IsZero() && !p.EffectiveFrom.Before(p.EffectiveTo) {
		return fmt.Errorf("profile effective interval must be nonempty")
	}
	if valueType(p.ValueType) == "" {
		return fmt.Errorf("unsupported profile value type %q", p.ValueType)
	}
	if p.Mode != Periodic && p.Mode != Event {
		return fmt.Errorf("reporting mode must be periodic or event")
	}
	if p.Mode == Periodic {
		if p.ScheduleAnchor.IsZero() || p.Period <= 0 {
			return fmt.Errorf("periodic reporting requires a fixed anchor and positive period")
		}
		// Compare without integer division, including odd-nanosecond periods.
		if p.Tolerance < 0 || p.Tolerance >= p.Period-p.Tolerance {
			return fmt.Errorf("tolerance must be nonnegative and strictly less than half the period")
		}
	}
	for name, v := range map[string]*float64{"minimum": p.Minimum, "maximum": p.Maximum, "epsilon": p.Epsilon, "maxRate": p.MaxRate, "MADMultiplier": p.MADMultiplier, "absoluteDeviation": p.AbsoluteDeviation} {
		if v != nil && !finite(*v) {
			return fmt.Errorf("%s must be finite", name)
		}
	}
	if p.RangeConfirmed && (!p.UnitConfirmed || p.Minimum == nil || p.Maximum == nil) {
		return fmt.Errorf("confirmed range requires confirmed unit and both limits")
	}
	if p.Minimum != nil && p.Maximum != nil && *p.Minimum > *p.Maximum {
		return fmt.Errorf("minimum must not exceed maximum")
	}
	if p.Epsilon != nil && *p.Epsilon <= 0 {
		return fmt.Errorf("epsilon must be positive")
	}
	if p.MaxRate != nil && *p.MaxRate < 0 {
		return fmt.Errorf("maximum rate must be nonnegative")
	}
	if p.StableDuration < 0 || p.MaxSequenceGap < 0 || (p.FutureTolerance != nil && *p.FutureTolerance < 0) {
		return fmt.Errorf("time tolerances and durations must be nonnegative")
	}
	if p.MinimumSamples != 0 && p.MinimumSamples < MinimumStatisticalSamples {
		return fmt.Errorf("statistical minimum samples must be at least %d", MinimumStatisticalSamples)
	}
	if p.DeviationMethod != "" && p.DeviationMethod != "mad" && p.DeviationMethod != "quantile" {
		return fmt.Errorf("deviation method must be mad or quantile")
	}
	if p.MADMultiplier != nil && *p.MADMultiplier <= 0 {
		return fmt.Errorf("MAD multiplier must be positive")
	}
	if p.AbsoluteDeviation != nil && *p.AbsoluteDeviation <= 0 {
		return fmt.Errorf("absolute deviation must be positive")
	}
	if p.DeviationMethod == "quantile" && (!finite(p.QuantileLower) || !finite(p.QuantileUpper) || p.QuantileLower < 0 || p.QuantileUpper > 1 || p.QuantileLower >= p.QuantileUpper) {
		return fmt.Errorf("quantiles must satisfy 0 <= lower < upper <= 1")
	}
	if p.CUSUM != nil {
		if p.CUSUM.Version == "" || !finite(p.CUSUM.Allowance) || !finite(p.CUSUM.Threshold) || p.CUSUM.Allowance < 0 || p.CUSUM.Threshold <= 0 {
			return fmt.Errorf("CUSUM requires a version, nonnegative finite allowance and positive finite threshold")
		}
	}
	return nil
}

func minimumSamples(p Profile) int {
	return max(MinimumStatisticalSamples, p.MinimumSamples)
}

// quantile uses linear interpolation at (n-1)*p (the inclusive sample quantile).
func quantile(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	x := float64(len(sorted)-1) * p
	i := int(math.Floor(x))
	j := min(i+1, len(sorted)-1)
	f := x - float64(i)
	// Convex form avoids overflowing b-a for opposite extreme finite values.
	return sorted[i]*(1-f) + sorted[j]*f
}

func statistics(values []float64) *Statistics {
	if len(values) == 0 {
		return nil
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	median := quantile(v, .5)
	deviations := make([]float64, len(v))
	for i, n := range v {
		deviations[i] = math.Abs(n - median)
		if !finite(deviations[i]) {
			// The finite input range can exceed float64 arithmetic. Keeping an
			// infinite statistic would make the frozen result unencodable.
			return nil
		}
	}
	sort.Float64s(deviations)
	return &Statistics{Count: len(v), Min: v[0], Max: v[len(v)-1], Median: median, MAD: quantile(deviations, .5), P05: quantile(v, .05), P25: quantile(v, .25), P75: quantile(v, .75), P95: quantile(v, .95)}
}

// BuildBaseline creates statistics only; it does not confirm a baseline.
// Confirmation belongs to an authenticated human operation in the application.
func BuildBaseline(in BaselineRequest) (Baseline, error) {
	if err := ValidateProfile(in.Profile); err != nil {
		return Baseline{}, err
	}
	b := in.Baseline
	if b.ID == "" || !b.Window.Start.Before(b.Window.End) || b.ProfileVersion != in.Profile.Version || !in.Profile.UnitConfirmed || b.Unit != in.Profile.Unit || b.OperatingCondition == "" || b.ProtocolVersion == "" || b.ConfigurationVersion == "" || b.ValidFrom.IsZero() || !b.ValidFrom.Before(b.ValidUntil) || b.Window.End.After(b.ValidFrom) {
		return Baseline{}, fmt.Errorf("baseline requires identity, window, confirmed unit, profile, protocol, configuration and operating condition")
	}
	values := make([]float64, 0, len(in.Samples))
	for _, s := range in.Samples {
		if !inWindow(s.EventAt, b.Window) {
			continue
		}
		if !validValue(in.Profile, s.Value) || s.Unit != b.Unit || s.ProtocolVersion != b.ProtocolVersion || s.ConfigurationVersion != b.ConfigurationVersion || s.OperatingCondition != b.OperatingCondition {
			return Baseline{}, fmt.Errorf("baseline contains invalid or mixed measurement types, units or versions")
		}
		n, ok := numeric(s.Value)
		if !ok {
			return Baseline{}, fmt.Errorf("statistical baseline requires numeric measurements")
		}
		values = append(values, n)
	}
	if len(values) < minimumSamples(in.Profile) {
		return Baseline{}, fmt.Errorf("baseline requires at least %d compatible samples", minimumSamples(in.Profile))
	}
	stats := statistics(values)
	if stats == nil {
		return Baseline{}, fmt.Errorf("baseline statistics exceed finite arithmetic range")
	}
	b.SampleCount, b.Median, b.MAD = len(values), stats.Median, stats.MAD
	sort.Float64s(values)
	if in.Profile.DeviationMethod == "quantile" {
		b.QuantileLower, b.QuantileUpper = in.Profile.QuantileLower, in.Profile.QuantileUpper
	} else {
		b.QuantileLower, b.QuantileUpper = .05, .95
	}
	b.LowerValue, b.UpperValue = quantile(values, b.QuantileLower), quantile(values, b.QuantileUpper)
	b.ConfirmedBy, b.ConfirmedAt = "", time.Time{}
	if in.Profile.CUSUM != nil {
		b.CUSUMVersion = in.Profile.CUSUM.Version
	}
	return b, nil
}

func inWindow(t time.Time, w Window) bool {
	return !t.IsZero() && !t.Before(w.Start) && t.Before(w.End)
}
