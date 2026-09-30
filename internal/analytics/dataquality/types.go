package dataquality

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"iot-platform/internal/analytics/quality"
	"iot-platform/internal/model"
	"math"
	"time"
)

const AlgorithmVersion = quality.AlgorithmVersion
const MaxRecords = 50000
const MaxSlots = 100000
const AttachmentBucket = "iot-quality-attachments"
const AttachmentMax = 16 << 20

func invalid(reason string) error { return fmt.Errorf("%w: %s", model.ErrAnalysisInvalid, reason) }
func instant(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
func profile(revision model.AnalysisConfigRevision) (quality.Profile, model.QualityProfile, error) {
	var body model.QualityProfile
	if err := strictDecode(revision.Body, &body); err != nil {
		return quality.Profile{}, body, err
	}
	if body.AttributeID == "" || body.EffectiveFrom <= 0 || body.ScheduleAnchor < 0 || body.PeriodMs < 0 || body.ToleranceMs < 0 || body.StableDurationMs < 0 || body.MaxSequenceGapMs < 0 {
		return quality.Profile{}, body, invalid("属性、时间或持续时长无效")
	}
	for _, ms := range []int64{body.PeriodMs, body.ToleranceMs, body.StableDurationMs, body.MaxSequenceGapMs} {
		if ms > int64(math.MaxInt64)/int64(time.Millisecond) {
			return quality.Profile{}, body, invalid("持续时长超限")
		}
	}
	p := quality.Profile{ID: revision.ID, Version: revision.ID, Mode: quality.Mode(body.Mode), EffectiveFrom: instant(body.EffectiveFrom), EffectiveTo: instant(body.EffectiveTo), ScheduleAnchor: instant(body.ScheduleAnchor), Period: time.Duration(body.PeriodMs) * time.Millisecond, Tolerance: time.Duration(body.ToleranceMs) * time.Millisecond, ValueType: body.ValueType, Required: body.Required, Unit: body.Unit, UnitConfirmed: body.UnitConfirmed, RangeConfirmed: body.RangeConfirmed, Minimum: body.Minimum, Maximum: body.Maximum, Epsilon: body.Epsilon, StableDuration: time.Duration(body.StableDurationMs) * time.Millisecond, MaxRate: body.MaxRate, MaxSequenceGap: time.Duration(body.MaxSequenceGapMs) * time.Millisecond, ClockCalibrated: body.ClockCalibrated, MinimumSamples: body.MinimumSamples, DeviationMethod: body.DeviationMethod, MADMultiplier: body.MADMultiplier, AbsoluteDeviation: body.AbsoluteDeviation, QuantileLower: body.QuantileLower, QuantileUpper: body.QuantileUpper}
	if body.FutureToleranceMs != nil {
		if *body.FutureToleranceMs < 0 || *body.FutureToleranceMs > int64(math.MaxInt64)/int64(time.Millisecond) {
			return p, body, invalid("时间容忍超限")
		}
		d := time.Duration(*body.FutureToleranceMs) * time.Millisecond
		p.FutureTolerance = &d
	}
	if body.CUSUM != nil {
		p.CUSUM = &quality.CUSUMConfig{Version: body.CUSUM.Version, Allowance: body.CUSUM.Allowance, Threshold: body.CUSUM.Threshold}
	}
	if err := quality.ValidateProfile(p); err != nil {
		return p, body, invalid(err.Error())
	}
	return p, body, nil
}
func baseline(revision model.AnalysisConfigRevision) (quality.Baseline, model.QualityBaseline, error) {
	var b model.QualityBaseline
	if err := json.Unmarshal(revision.Body, &b); err != nil {
		return quality.Baseline{}, b, err
	}
	return quality.Baseline{ID: revision.ID, ProfileVersion: b.ProfileVersion, ProtocolVersion: b.ProtocolVersion, ConfigurationVersion: b.ConfigurationVersion, Unit: b.Unit, OperatingCondition: b.OperatingCondition, Window: quality.Window{Start: instant(b.Start), End: instant(b.End)}, ValidFrom: instant(b.ValidFrom), ValidUntil: instant(b.ValidUntil), SampleCount: b.SampleCount, Median: b.Median, MAD: b.MAD, QuantileLower: b.QuantileLower, QuantileUpper: b.QuantileUpper, LowerValue: b.LowerValue, UpperValue: b.UpperValue, ConfirmedBy: b.ConfirmedBy, ConfirmedAt: instant(b.ConfirmedAt), CUSUMVersion: b.CUSUMVersion}, b, nil
}
func sample(f model.MeasurementFact) quality.Sample {
	return quality.Sample{ID: f.ID, MessageID: f.MessageID, RawMessageID: f.RawMessageID, EventAt: instant(f.EventAt), ReceivedAt: instant(f.ReceivedAt), AvailableAt: instant(f.AvailableAt), Value: f.Value, Unit: f.Unit, ProtocolVersion: f.ProtocolVersion, ConfigurationVersion: f.ConfigurationVersion, OperatingCondition: f.OperatingCondition}
}
func strictDecode(data json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return invalid(err.Error())
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return invalid("仅允许一个JSON对象")
	}
	return nil
}

// API output times remain Unixms even though the pure algorithm uses time.Time.
func publicJSON(value any) json.RawMessage {
	b, _ := json.Marshal(value)
	var data any
	_ = json.Unmarshal(b, &data)
	convertTimes(data)
	result, _ := json.Marshal(data)
	return result
}
func convertTimes(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if s, ok := item.(string); ok && isTimeKey(key) {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					if t.IsZero() {
						v[key] = int64(0)
					} else {
						v[key] = t.UnixMilli()
					}
					continue
				}
			}
			convertTimes(item)
		}
	case []any:
		for _, item := range v {
			convertTimes(item)
		}
	}
}

func isTimeKey(key string) bool {
	switch key {
	case "start", "end", "at", "eventAt", "receivedAt", "availableAt", "effectiveFrom", "effectiveTo", "scheduleAnchor", "confirmedAt", "validFrom", "validUntil", "calibratedAt", "recordedAt", "occurredAt":
		return true
	}
	return false
}
