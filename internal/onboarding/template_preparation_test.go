package onboarding

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func verificationFixture(t *testing.T, rules model.VerificationRules) (*Service, *memory.Repository, map[string]model.RawMessage) {
	t.Helper()
	ctx := context.Background()
	repo := memory.NewRepository()
	p := model.Product{ID: "p", TenantID: "t", Name: "烟感", Status: "ENABLED", Transport: "HTTP", ProtocolPackageID: StandardPackageID, VerificationRules: &rules, CreatedAt: time.Now().Add(-48 * time.Hour).UnixMilli()}
	if err := repo.SaveProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Name: "现场烟感", Status: "ENABLED", Connector: "HTTP", CreatedAt: p.CreatedAt}); err != nil {
		t.Fatal(err)
	}
	raws := map[string]model.RawMessage{}
	s := New(repo, nil, "", nil)
	s.LoadRaw = func(_ context.Context, idx model.RawArchiveIndex) (model.RawMessage, error) {
		raw, ok := raws[idx.MessageID]
		if !ok {
			return raw, model.ErrNotFound
		}
		return raw, nil
	}
	return s, repo, raws
}

func putVerificationEvidence(t *testing.T, repo *memory.Repository, raws map[string]model.RawMessage, id, source, version string, at int64, kind model.MessageType, event string, parsed bool) {
	t.Helper()
	raws[id] = model.RawMessage{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: id, Source: source, ProtocolID: parser.StandardProtocolID, ProtocolVersion: version}
	if _, err := repo.SaveRawIndex(context.Background(), model.RawArchiveIndex{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: id, ReceivedAt: at}); err != nil {
		t.Fatal(err)
	}
	if parsed {
		if err := repo.SaveStandardMessage(context.Background(), model.StandardMessage{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: "std-" + id, RawMessageID: id, MessageType: kind, Properties: map[string]any{"temperature": 25}, Event: map[string]any{"id": event}, Timestamp: at}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTemplateVerificationUsesFieldVersionAndIndependentRules(t *testing.T) {
	rules := model.VerificationRules{Mode: "event", MinMessages: 1, WindowSeconds: 86400, RequiredEvents: []string{"self_test"}, RequiredProperties: []string{"temperature"}}
	s, repo, raws := verificationFixture(t, rules)
	now := time.Now().UnixMilli()
	ctx := context.Background()
	for _, row := range []struct{ id, source, version string }{{"sim", "managed-device", "1.0.0"}, {"replay", "replay", "1.0.0"}, {"old", "standard-http", "0.9.0"}, {"unknown-version", "standard-http", ""}} {
		putVerificationEvidence(t, repo, raws, row.id, row.source, row.version, now-1000, model.EventReport, "self_test", true)
	}
	result, err := s.VerifyDevice(ctx, "t", "d")
	if err != nil || result.Status == "VERIFIED" || result.MessageCount != 0 {
		t.Fatalf("nonfield evidence accepted: %+v %v", result, err)
	}
	putVerificationEvidence(t, repo, raws, "field", "standard-http", "1.0.0", now-int64(time.Hour/time.Millisecond), model.EventReport, "self_test", true)
	result, err = s.VerifyDevice(ctx, "t", "d")
	if err != nil || result.Status != "VERIFIED" || result.MessageCount != 1 {
		t.Fatalf("event device wrongly requires two reports/15 minutes: %+v %v", result, err)
	}
	putVerificationEvidence(t, repo, raws, "failed", "standard-http", "1.0.0", now, model.EventReport, "", false)
	result, err = s.VerifyDevice(ctx, "t", "d")
	if err != nil || result.Status == "VERIFIED" {
		t.Fatalf("latest parse failure must block: %+v %v", result, err)
	}
}

func TestTemplateVerificationDoesNotCarryEvidenceAcrossConfiguration(t *testing.T) {
	s, repo, raws := verificationFixture(t, model.VerificationRules{Mode: "low_frequency", MinMessages: 1, WindowSeconds: 86400})
	ctx := context.Background()
	now := time.Now().UnixMilli()
	putVerificationEvidence(t, repo, raws, "one", "standard-http", "1.0.0", now-1000, model.PropertyReport, "", true)
	result, err := s.VerifyDevice(ctx, "t", "d")
	if err != nil || result.Status != "VERIFIED" {
		t.Fatal(result, err)
	}
	rec, prep, err := s.TemplateRecord(ctx, "t", "p")
	if err != nil {
		t.Fatal(err)
	}
	prep.Status = "READY"
	prep.Fingerprint = result.Fingerprint
	prep.Verification = &result
	if _, err = s.SaveTemplateRecord(ctx, rec, prep); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := s.TemplateReadiness(ctx, "t", "p"); err != nil || !ready {
		t.Fatal("not ready", err)
	}
	p, _ := repo.GetProduct(ctx, "t", "p")
	p.Name = "改名"
	p.Description = "只改说明"
	p.UpdatedAt = now
	if err = repo.SaveProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := s.TemplateReadiness(ctx, "t", "p"); err != nil || !ready {
		t.Fatal("cosmetic edit invalidated evidence", err)
	}
	p.VerificationRules = &model.VerificationRules{Mode: "periodic", MinMessages: 3, WindowSeconds: 600, MaxGapSeconds: 100}
	if err = repo.SaveProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	if status, ready, err := s.TemplateReadiness(ctx, "t", "p"); err != nil || ready || status != "CONFIGURATION_CHANGED" {
		t.Fatal(status, ready, err)
	}
	if _, err = s.VerifyDevice(ctx, "foreign", "d"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("cross tenant", err)
	}
}

func TestTemplateFingerprintPinsStandardPlanPublicAddress(t *testing.T) {
	for _, transport := range []string{"MQTT_HTTP", "", "MQTT", "HTTP"} {
		t.Run("transport-"+transport, func(t *testing.T) {
			s, repo, _ := verificationFixture(t, model.VerificationRules{Mode: "low_frequency", MinMessages: 1, WindowSeconds: 86400})
			ctx := context.Background()
			s.PublicHTTP, s.PublicMQTT = "https://first.example.test", "mqtts://first.example.test:8883"
			product, err := repo.GetProduct(ctx, "t", "p")
			if err != nil {
				t.Fatal(err)
			}
			product.Transport = transport
			if err = repo.SaveProduct(ctx, product); err != nil {
				t.Fatal(err)
			}
			plan, err := s.Plan(ctx, "t", product)
			if err != nil {
				t.Fatal(err)
			}
			fingerprint, err := s.TemplateFingerprint(ctx, "t", "p")
			if err != nil {
				t.Fatal(err)
			}
			record, prep, err := s.TemplateRecord(ctx, "t", "p")
			if err != nil {
				t.Fatal(err)
			}
			prep.Status, prep.Fingerprint = "READY", fingerprint
			prep.Verification = &model.DeviceVerification{Status: "VERIFIED", Fingerprint: fingerprint}
			if _, err = s.SaveTemplateRecord(ctx, record, prep); err != nil {
				t.Fatal(err)
			}
			// The unused endpoint cannot invalidate the selected default channel.
			if plan.Connector == "MQTT" {
				s.PublicHTTP = "https://unused-changed.example.test"
			} else {
				s.PublicMQTT = "mqtts://unused-changed.example.test:8883"
			}
			if _, ready, err := s.TemplateReadiness(ctx, "t", "p"); err != nil || !ready {
				t.Fatal("fingerprint used a different endpoint than the access plan", plan.Connector, err)
			}
			if plan.Connector == "MQTT" {
				s.PublicMQTT = "mqtts://selected-changed.example.test:8883"
			} else {
				s.PublicHTTP = "https://selected-changed.example.test"
			}
			if state, ready, err := s.TemplateReadiness(ctx, "t", "p"); err != nil || ready || state != "CONFIGURATION_CHANGED" {
				t.Fatal("public endpoint change reused old acceptance", plan.Connector, state, ready, err)
			}
		})
	}
}

func TestTemplateVerificationPeriodicGapAndRequiredTypes(t *testing.T) {
	s, repo, raws := verificationFixture(t, model.VerificationRules{Mode: "periodic", MinMessages: 2, WindowSeconds: 7200, MaxGapSeconds: 120, RequiredMessageTypes: []string{"PROPERTY_REPORT", "STATE_CHANGE"}})
	now := time.Now().UnixMilli()
	ctx := context.Background()
	putVerificationEvidence(t, repo, raws, "a", "standard-http", "1.0.0", now-600000, model.PropertyReport, "", true)
	putVerificationEvidence(t, repo, raws, "b", "standard-http", "1.0.0", now-1000, model.StateChange, "", true)
	result, err := s.VerifyDevice(ctx, "t", "d")
	if err != nil || result.Status == "VERIFIED" {
		t.Fatal("long gap accepted", result, err)
	}
	putVerificationEvidence(t, repo, raws, "c", "standard-http", "1.0.0", now, model.PropertyReport, "", true)
	result, err = s.VerifyDevice(ctx, "t", "d")
	if err != nil || result.Status != "VERIFIED" {
		t.Fatal(result, err)
	}
}

func TestTemplateVerificationRejectsLateFramesFromPreviousConnection(t *testing.T) {
	s, repo, raws := verificationFixture(t, model.VerificationRules{Mode: "low_frequency", MinMessages: 1, WindowSeconds: 86400})
	ctx := context.Background()
	now := time.Now().UnixMilli()
	profile := model.DeviceAccessProfile{TenantID: "t", ID: "poll", ProductID: "p", DeviceID: "d", Host: "old-device.local", Port: 502, UpdatedAt: now - 1000}
	oldFingerprint := profile.ConfigurationFingerprint()
	profile.Host = "current-device.local"
	if err := repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	d, _ := repo.GetManagedDevice(ctx, "t", "d")
	d.ConnectorProfileID = profile.ID
	if err := repo.SaveManagedDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	putVerificationEvidence(t, repo, raws, "late", "modbus-tcp-collector", "1.0.0", now, model.PropertyReport, "", true)
	raw := raws["late"]
	raw.Metadata = map[string]any{"profileId": profile.ID, "profileFingerprint": oldFingerprint}
	raws["late"] = raw
	result, err := s.VerifyDevice(ctx, "t", "d")
	if err != nil || result.Status == "VERIFIED" || result.MessageCount != 0 {
		t.Fatal("late old-connection data accepted", result, err)
	}
	raw.Metadata["profileFingerprint"] = profile.ConfigurationFingerprint()
	raws["late"] = raw
	result, err = s.VerifyDevice(ctx, "t", "d")
	if err != nil || result.Status != "VERIFIED" || result.ProfileFingerprint != profile.ConfigurationFingerprint() {
		t.Fatal("current connection evidence rejected", result, err)
	}
}

func TestTemplateVerificationAcceptsStandardEventIdentifier(t *testing.T) {
	s, repo, raws := verificationFixture(t, model.VerificationRules{Mode: "event", MinMessages: 1, WindowSeconds: 86400, RequiredEvents: []string{"selfTest"}})
	ctx := context.Background()
	now := time.Now().UnixMilli()
	putVerificationEvidence(t, repo, raws, "event", "standard-http", "1.0.0", now, model.EventReport, "", false)
	raw := raws["event"]
	raw.ReceivedAt = now
	raw.Headers = map[string]string{"messageKind": "event"}
	raw.Payload = []byte(fmt.Sprintf(`{"id":"sample-event","timestamp":%d,"version":"1.0","event":"selfTest","data":{"result":"ok"}}`, now))
	message, err := (parser.StandardParser{}).Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveStandardMessage(ctx, *message); err != nil {
		t.Fatal(err)
	}
	raws["event"] = raw
	result, err := s.VerifyDevice(ctx, "t", "d")
	if err != nil || result.Status != "VERIFIED" {
		t.Fatal("standard event identifier was not recognized", result, err)
	}
}
