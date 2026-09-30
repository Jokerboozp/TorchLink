// Package testkit creates explicitly synthetic, isolated quality fixtures. It
// does not register production receivers or alter any production alarm state.
package testkit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type SeedResult struct {
	TenantID, DeviceID, ProductID, AttributeID string
	Start, End                                 int64
	ProfileRequest                             model.QualityConfigRequest
	BaselineRequest                            model.QualityBaselineRequest
}

func Seed(ctx context.Context, repo ports.Repository, tenant string, count int) (SeedResult, error) {
	return SeedAt(ctx, repo, tenant, count, time.Now().Add(-2*time.Minute).UnixMilli())
}

// SeedAt permits whole-second UI fixture boundaries without altering the
// actual availability acknowledgement clock of the underlying stores.
func SeedAt(ctx context.Context, repo ports.Repository, tenant string, count int, start int64) (SeedResult, error) {
	if count < 30 || count > 1000 {
		return SeedResult{}, fmt.Errorf("synthetic sample count must be 30..1000")
	}
	if start <= 0 {
		return SeedResult{}, fmt.Errorf("synthetic sample start must be positive")
	}
	result := SeedResult{TenantID: tenant, DeviceID: "quality-device", ProductID: "quality-product", AttributeID: "pressure", Start: start, End: start + int64(count)*1000}
	product := model.Product{TenantID: tenant, ID: result.ProductID, Name: "隔离数据质量验证产品", Status: "ENABLED", ThingModel: &model.ThingModel{Properties: []model.ThingField{{Identifier: "pressure", Name: "压力", DataType: "number", Unit: "kPa", Required: true}}}}
	if err := repo.SaveProduct(ctx, product); err != nil {
		return result, err
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ID: result.DeviceID, ProductID: result.ProductID, Name: "隔离样本设备", Status: "ENABLED", AccessKey: tenant + "-fixture-key"}); err != nil {
		return result, err
	}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("quality-fixture-%04d", i)
		value := 100.0 + float64(i%5)
		payload, _ := json.Marshal(map[string]any{"pressure": value})
		raw := model.RawMessage{TenantID: tenant, MessageID: "raw-" + id, DeviceID: result.DeviceID, ProductID: result.ProductID, Source: "isolated-synthetic-quality-test", Protocol: "synthetic-json", ProtocolVersion: "quality-protocol-v1", PointTableVersion: "quality-points-v1", ReceivedAt: start + int64(i)*1000 + 20, Payload: payload}
		if _, err := repo.ReserveRawMessage(ctx, raw); err != nil {
			return result, err
		}
		if database, ok := repo.(ports.RawMessageDatabase); ok {
			if err := database.SaveRawMessage(ctx, raw); err != nil {
				return result, err
			}
		}
		if _, err := repo.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: tenant, MessageID: raw.MessageID, DeviceID: raw.DeviceID, ProductID: raw.ProductID, ReceivedAt: raw.ReceivedAt, ArchivedAt: time.Now().UnixMilli(), ObjectBucket: "postgres", ObjectKey: raw.MessageID, Protocol: raw.Protocol, PayloadHash: raw.PayloadHash(), PayloadSize: len(payload)}); err != nil {
			return result, err
		}
		message := model.StandardMessage{TenantID: tenant, MessageID: id, RawMessageID: raw.MessageID, DeviceID: raw.DeviceID, ProductID: raw.ProductID, MessageType: model.PropertyReport, Timestamp: start + int64(i)*1000, Properties: map[string]any{"pressure": value}, Tags: map[string]string{"unit:pressure": "kPa", "configurationVersion": "quality-config-v1", "operatingCondition": "人工确认的隔离验证工况"}, Parser: "synthetic-json", ParserVersion: "fixture-v1"}
		if _, err := repo.SaveStandardMessageIfAbsent(ctx, message); err != nil {
			return result, err
		}
		if err := repo.MarkRawParseResult(ctx, tenant, raw.MessageID, time.Now().UnixMilli(), ""); err != nil {
			return result, err
		}
	}
	minv, maxv, epsilon := 0.0, 500.0, .1
	body, _ := json.Marshal(model.QualityProfile{AttributeID: "pressure", Mode: "periodic", EffectiveFrom: start, ScheduleAnchor: start, PeriodMs: 1000, ToleranceMs: 100, ValueType: "number", Required: true, Unit: "kPa", UnitConfirmed: true, RangeConfirmed: true, Minimum: &minv, Maximum: &maxv, Epsilon: &epsilon, StableDurationMs: 20000, MaxSequenceGapMs: 2000, MinimumSamples: 30, DeviationMethod: "mad"})
	result.ProfileRequest = model.QualityConfigRequest{ResourceID: "quality-profile", Scope: "personal", DeviceIDs: []string{result.DeviceID}, Body: body}
	result.BaselineRequest = model.QualityBaselineRequest{DeviceID: result.DeviceID, AttributeID: result.AttributeID, Start: start, End: result.End, ValidFrom: result.End, ValidUntil: result.End + 24*time.Hour.Milliseconds(), ProtocolVersion: "quality-protocol-v1", ConfigurationVersion: "quality-config-v1", OperatingCondition: "人工确认的隔离验证工况"}
	return result, nil
}
