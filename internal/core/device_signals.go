package core

import (
	"context"
	"encoding/json"
	"fmt"
	"iot-platform/internal/logkey"
	"math"
	"sort"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Device health signals are computed from reported data by the jobs role:
// stuck numeric values, report intervals drifting from the configured
// interval, values outside the thing model's valid range, and devices whose
// averages stand out among devices of the same template. They need no model.
const (
	defaultSignalWindow    = 24 * time.Hour
	defaultSignalTenantRun = 2 * time.Minute
	// A numeric property is stuck when at least this many reports carry the
	// same value.
	signalStuckMinReports = 10
	// Report drift needs this many reports and an average interval this far
	// (as a ratio) from the configured interval.
	signalDriftMinReports = 3
	signalDriftRatio      = 0.5
	// Out-of-range values must be at least this share of a property's values.
	signalOutOfRangeShare = 0.05
	// Peer outliers need this many devices of one template and a modified
	// z-score (median and median absolute deviation, Iglewicz and Hoaglin)
	// of at least signalPeerZ, so one outlier cannot hide by inflating the
	// spread it is measured against.
	signalPeerMinDevices = 5
	signalPeerZ          = 3.5
	// signalAlarmStrength is the strength from which a signal raises an alarm
	// when signal alarms are enabled.
	signalAlarmStrength = 0.5
	// SignalAlarmType and signalAlarmSource mark alarms raised from signals.
	SignalAlarmType   = "DEVICE_HEALTH"
	signalAlarmSource = "device-signal"
	signalStatePage   = 500
)

// DeviceSignalOptions configures the device-signals job.
type DeviceSignalOptions struct {
	// Window is how far back reports are read; zero is 24 hours.
	Window time.Duration
	// TenantTimeout bounds one tenant's computation; zero is 2 minutes.
	TenantTimeout time.Duration
	// RaiseAlarms turns strong signals into DEVICE_HEALTH alarms; by default
	// signals are only recorded.
	RaiseAlarms bool
}

// ComputeDeviceSignalsOnce recomputes every tenant's signals. A tenant that
// fails or exceeds its time limit keeps its previous signals.
func (e *Engine) ComputeDeviceSignalsOnce(ctx context.Context) error {
	if e.DeviceSignals == nil || e.TelemetryStats == nil {
		return nil
	}
	tenants, err := e.DeviceSignals.SignalTenants(ctx)
	if err != nil {
		return err
	}
	var failed error
	for _, tenant := range tenants {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		timeout := e.SignalOptions.TenantTimeout
		if timeout <= 0 {
			timeout = defaultSignalTenantRun
		}
		tenantCtx, cancel := context.WithTimeout(ctx, timeout)
		started := time.Now()
		signals, err := e.ComputeTenantSignals(tenantCtx, tenant)
		if err == nil {
			err = e.DeviceSignals.ReplaceDeviceSignals(tenantCtx, tenant, signals)
		}
		if err == nil && e.SignalOptions.RaiseAlarms {
			err = e.syncSignalAlarms(tenantCtx, tenant, signals)
		}
		cancel()
		if err != nil {
			failed = fmt.Errorf("tenant %s device signals: %w", tenant, err)
			if e.Log != nil {
				e.Log.Warn("device signals failed", logkey.Tenant, tenant, "error", err)
			}
			continue
		}
		if e.Log != nil {
			e.Log.Debug("device signals computed", logkey.Tenant, tenant, "signals", len(signals), "duration", time.Since(started))
		}
	}
	return failed
}

// ComputeTenantSignals derives a tenant's signals from the window's reports.
func (e *Engine) ComputeTenantSignals(ctx context.Context, tenant string) ([]model.DeviceSignal, error) {
	window := e.SignalOptions.Window
	if window <= 0 {
		window = defaultSignalWindow
	}
	now := e.Clock.Now()
	start, end := now.Add(-window).UnixMilli(), now.UnixMilli()
	products, err := e.Repo.ListProducts(ctx, tenant)
	if err != nil {
		return nil, err
	}
	productByID := map[string]model.Product{}
	ranges := []model.PropertyRange{}
	for _, p := range products {
		productByID[p.ID] = p
		if p.ThingModel == nil {
			continue
		}
		for _, f := range p.ThingModel.Properties {
			if f.Numeric() && (f.Min != nil || f.Max != nil) {
				ranges = append(ranges, model.PropertyRange{ProductID: p.ID, Property: f.Identifier, Min: f.Min, Max: f.Max})
			}
		}
	}
	stats, err := e.TelemetryStats.DevicePropertyStats(ctx, tenant, start, end, ranges)
	if err != nil {
		return nil, err
	}
	reports, err := e.TelemetryStats.DeviceReportStats(ctx, tenant, start, end)
	if err != nil {
		return nil, err
	}
	signals := []model.DeviceSignal{}
	add := func(device, product, signalType, property string, strength float64, evidence map[string]any) {
		signals = append(signals, model.DeviceSignal{TenantID: tenant, DeviceID: device, ProductID: product, SignalType: signalType, Property: property,
			Strength: math.Round(min(max(strength, 0), 1)*1000) / 1000, WindowStart: start, WindowEnd: end, Evidence: evidence, UpdatedAt: end})
	}
	peers := map[[2]string][]model.DevicePropertyStat{}
	for _, s := range stats {
		if s.Count >= signalStuckMinReports && s.StdDev == 0 {
			add(s.DeviceID, s.ProductID, model.SignalStuckValue, s.Property, float64(s.Count)/50, map[string]any{"reports": s.Count, "value": s.Min})
		}
		if s.Count > 0 && s.OutOfRange > 0 {
			if share := float64(s.OutOfRange) / float64(s.Count); share >= signalOutOfRangeShare {
				add(s.DeviceID, s.ProductID, model.SignalOutOfRange, s.Property, share, map[string]any{"reports": s.Count, "outOfRange": s.OutOfRange, "min": s.Min, "max": s.Max})
			}
		}
		peers[[2]string{s.ProductID, s.Property}] = append(peers[[2]string{s.ProductID, s.Property}], s)
	}
	for key, group := range peers {
		if len(group) < signalPeerMinDevices {
			continue
		}
		means := make([]float64, len(group))
		for i, s := range group {
			means[i] = s.Mean
		}
		median := medianOf(means)
		deviations := make([]float64, len(group))
		for i, v := range means {
			deviations[i] = math.Abs(v - median)
		}
		mad := medianOf(deviations)
		if mad == 0 {
			continue
		}
		for _, s := range group {
			if z := 0.6745 * math.Abs(s.Mean-median) / mad; z >= signalPeerZ {
				add(s.DeviceID, key[0], model.SignalPeerOutlier, key[1], z/(2*signalPeerZ), map[string]any{"mean": s.Mean, "peerMedian": median, "zScore": math.Round(z*100) / 100, "peers": len(group)})
			}
		}
	}
	intervals, err := e.expectedIntervals(ctx, tenant, reports, productByID)
	if err != nil {
		return nil, err
	}
	for _, r := range reports {
		expected := intervals[r.DeviceID]
		if r.Count < signalDriftMinReports || expected <= 0 {
			continue
		}
		average := float64(r.LastAt-r.FirstAt) / float64(r.Count-1) / 1000
		if ratio := average / float64(expected); math.Abs(ratio-1) >= signalDriftRatio {
			add(r.DeviceID, r.ProductID, model.SignalReportDrift, "", math.Abs(ratio-1)/2, map[string]any{"reports": r.Count, "averageIntervalSec": math.Round(average), "expectedIntervalSec": expected})
		}
	}
	sort.Slice(signals, func(i, j int) bool {
		a, b := signals[i], signals[j]
		if a.DeviceID != b.DeviceID {
			return a.DeviceID < b.DeviceID
		}
		if a.SignalType != b.SignalType {
			return a.SignalType < b.SignalType
		}
		return a.Property < b.Property
	})
	return signals, nil
}

// expectedIntervals reads each reporting device's configured interval from
// its state, falling back to its template's.
func (e *Engine) expectedIntervals(ctx context.Context, tenant string, reports []model.DeviceReportStat, products map[string]model.Product) (map[string]int64, error) {
	out := make(map[string]int64, len(reports))
	for i := 0; i < len(reports); i += signalStatePage {
		batch := reports[i:min(i+signalStatePage, len(reports))]
		ids := make([]string, len(batch))
		for j, r := range batch {
			ids[j] = r.DeviceID
		}
		states, err := e.Repo.GetDeviceStatesByIDs(ctx, tenant, ids)
		if err != nil {
			return nil, err
		}
		for _, r := range batch {
			if state, ok := states[r.DeviceID]; ok && state.ReportIntervalSec > 0 {
				out[r.DeviceID] = state.ReportIntervalSec
				continue
			}
			product := products[r.ProductID]
			out[r.DeviceID], _ = EffectiveDeviceTiming(&product, nil)
		}
	}
	return out, nil
}

func signalRuleID(s model.DeviceSignal) string {
	return signalAlarmSource + ":" + s.SignalType + ":" + s.Property
}

// SignalTypeNames are the Chinese names of signal types, used in alarm
// content, inspection findings and analysis context.
var SignalTypeNames = map[string]string{model.SignalStuckValue: "数值长时间不变", model.SignalReportDrift: "上报周期偏离", model.SignalOutOfRange: "数值超出有效范围", model.SignalPeerOutlier: "与同型号设备差异显著"}

// syncSignalAlarms raises a DEVICE_HEALTH alarm for each strong signal and
// recovers signal alarms whose signal has gone.
func (e *Engine) syncSignalAlarms(ctx context.Context, tenant string, signals []model.DeviceSignal) error {
	strong := map[string]model.DeviceSignal{}
	for _, s := range signals {
		if s.Strength >= signalAlarmStrength {
			strong[s.DeviceID+"\x00"+signalRuleID(s)] = s
		}
	}
	open := map[string]bool{}
	err := e.Repo.EachAlarm(ctx, ports.AlarmFilter{TenantID: tenant, Source: signalAlarmSource, Summary: true}, func(a model.Alarm) error {
		if a.Status != "ACTIVE" && a.Status != "ACKED" {
			return nil
		}
		key := a.DeviceID + "\x00" + a.RuleID
		if _, ok := strong[key]; ok {
			open[key] = true
			return nil
		}
		_, err := e.SetAlarmStatus(ctx, tenant, a.ID, "RECOVERED", "device-signals")
		return err
	})
	if err != nil {
		return err
	}
	for key, s := range strong {
		if open[key] {
			continue
		}
		now := e.Clock.Now().UnixMilli()
		name := SignalTypeNames[s.SignalType]
		content := name
		if s.Property != "" {
			content = s.Property + " " + name
		}
		a := model.Alarm{ID: id("alarm"), TenantID: tenant, RuleID: signalRuleID(s), DeviceID: s.DeviceID, DeviceName: e.alarmDeviceName(ctx, tenant, s.DeviceID), AlarmType: SignalAlarmType, AlarmLevel: "LOW", Status: "ACTIVE",
			Source: signalAlarmSource, DeviceType: s.ProductID, FirstTriggeredAt: now, LastTriggeredAt: now, TriggerCount: 1, Content: content, Details: map[string]any{"signal": s}}
		a.Location = e.alarmLocation(ctx, tenant, s.DeviceID, "")
		saved, created, err := e.Repo.UpsertAlarm(ctx, a)
		if err != nil {
			return err
		}
		if !created {
			continue
		}
		if err := e.syncDeviceBusinessStatus(ctx, tenant, s.ProductID, s.DeviceID); err != nil {
			return err
		}
		payload, _ := json.Marshal(saved)
		_ = e.Bus.Publish(ctx, model.TopicAlarmRaised, saved.ID, payload)
		if e.Realtime != nil {
			_ = e.Realtime.Publish(ctx, saved.MQTTTopic("raised"), payload, 1, false)
		}
	}
	return nil
}

func medianOf(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
