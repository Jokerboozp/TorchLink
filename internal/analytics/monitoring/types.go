package monitoring

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/continuity"
	"iot-platform/internal/model"
)

const AlgorithmVersion = continuity.AlgorithmVersion
const MaxRecords = 50000
const MaxIntervals = 100000
const MaxPairs = 10000

func invalid(reason string) error { return fmt.Errorf("%w: %s", model.ErrAnalysisInvalid, reason) }
func decode(body json.RawMessage, v any) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return invalid(err.Error())
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return invalid("仅允许一个JSON对象")
	}
	return nil
}
func profile(v model.AnalysisConfigRevision) (continuity.Profile, model.MonitoringProfile, error) {
	var b model.MonitoringProfile
	if err := decode(v.Body, &b); err != nil {
		return continuity.Profile{}, b, err
	}
	p := continuity.Profile{ID: v.ID, ProductID: b.ProductID, EffectiveFrom: b.EffectiveFrom, EffectiveTo: b.EffectiveTo, Mode: b.Mode, Merge: b.Merge, PeriodMs: b.PeriodMs, ToleranceMs: b.ToleranceMs, MessageTypes: b.MessageTypes, Importance: b.Importance, LongGapMs: b.LongGapMs, FrequentGapCount: b.FrequentGapCount}
	for _, a := range b.Attributes {
		p.Attributes = append(p.Attributes, continuity.Attribute{ID: a.ID, ValueType: a.ValueType, Minimum: a.Minimum, Maximum: a.Maximum})
	}
	if err := continuity.ValidateProfile(p); err != nil {
		return p, b, invalid(err.Error())
	}
	for _, kind := range b.MessageTypes {
		if !slices.Contains([]model.MessageType{model.PropertyReport, model.EventReport, model.AlarmReport, model.StateChange, model.CommandReply, model.LogReport}, kind) {
			return p, b, invalid("不可接受未知报文类型")
		}
	}
	return p, b, nil
}
func commonPolicy(b *model.MonitoringCommonGapPolicy) continuity.CommonGapPolicy {
	if b == nil {
		return continuity.CommonGapPolicy{}
	}
	return continuity.CommonGapPolicy{Version: b.Version, MinimumGapCount: b.MinimumGapCount, MinimumOverlapMs: b.MinimumOverlapMs, MinimumJaccard: b.MinimumJaccard}
}
func configurationHash(revisions []model.AnalysisConfigRevision, qualitySnapshots []model.AnalysisSnapshot) (string, error) {
	type configIdentity struct {
		ID, Kind, Scope, Creator, Hash string
		Version                        int64
	}
	items := []configIdentity{}
	for _, v := range revisions {
		hash, err := analytics.AnalysisHash(v.Body)
		if err != nil {
			return "", err
		}
		items = append(items, configIdentity{v.ID, v.Kind, v.Scope, v.Creator, hash, v.Version})
	}
	slices.SortFunc(items, func(a, b configIdentity) int { return strings.Compare(a.ID, b.ID) })
	type qualityIdentity struct {
		ID, RunID, FactsHash string
		Version              int64
	}
	quality := []qualityIdentity{}
	for _, v := range qualitySnapshots {
		quality = append(quality, qualityIdentity{v.ID, v.RunID, v.FactsHash, v.Version})
	}
	slices.SortFunc(quality, func(a, b qualityIdentity) int { return strings.Compare(a.ID, b.ID) })
	return analytics.AnalysisHash(struct {
		Configs []configIdentity
		Quality []qualityIdentity
	}{items, quality})
}
func compatibleType(kind, expected string) bool {
	if expected == "integer" {
		return slices.Contains([]string{"int", "integer", "int32", "int64", "long"}, strings.ToLower(kind))
	}
	switch strings.ToLower(kind) {
	case "int", "integer", "int32", "int64", "long", "float", "float32", "float64", "double", "number", "decimal":
		kind = "number"
	case "bool", "boolean":
		kind = "boolean"
	case "text", "string":
		kind = "string"
	}
	return kind == expected
}
