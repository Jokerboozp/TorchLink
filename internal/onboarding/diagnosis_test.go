package onboarding

import (
	"testing"

	"iot-platform/internal/model"
)

func TestDiagnoseOrdersFixesBeforeDataStages(t *testing.T) {
	listening := &model.DeviceAccessProfile{Mode: "listener", Enabled: true, RuntimeStatus: "LISTENING", PublicHost: "iot.example.com"}
	ready := DiagnosisInput{ProductEnabled: true, ProtocolValid: true, DeviceEnabled: true, Profile: listening}
	cases := []struct {
		name  string
		edit  func(*DiagnosisInput)
		stage string
		tone  string
	}{
		{"product disabled wins", func(in *DiagnosisInput) { in.ProductEnabled = false; in.Ingest.Parsed = true }, "PRODUCT_DISABLED", "warning"},
		{"protocol invalid", func(in *DiagnosisInput) { in.ProtocolValid = false }, "PROTOCOL_INVALID", "warning"},
		{"device disabled", func(in *DiagnosisInput) { in.DeviceEnabled = false }, "DEVICE_DISABLED", "warning"},
		{"child without visible parent", func(in *DiagnosisInput) { in.IsChild = true }, "PARENT_UNAVAILABLE", "warning"},
		{"protocol device without connection", func(in *DiagnosisInput) { in.Profile = nil }, "PROFILE_MISSING", "warning"},
		{"disabled listener", func(in *DiagnosisInput) { p := *listening; p.Enabled = false; in.Profile = &p }, "PROFILE_DISABLED", "warning"},
		{"listener runtime error", func(in *DiagnosisInput) { p := *listening; p.RuntimeStatus = "ERROR"; in.Profile = &p }, "PROFILE_ERROR", "error"},
		{"listener without public host", func(in *DiagnosisInput) { p := *listening; p.PublicHost = ""; in.Profile = &p }, "PUBLIC_HOST_MISSING", "warning"},
		{"dial profile needs no public host", func(in *DiagnosisInput) {
			p := *listening
			p.PublicHost = ""
			p.ConnectionMode = "dial"
			in.Profile = &p
		}, "WAITING", "info"},
		{"parse failure", func(in *DiagnosisInput) { in.Ingest = IngestSummary{RawReceived: true, ParseError: "bad frame"} }, "PARSE_FAILED", "error"},
		{"raw waiting for parser", func(in *DiagnosisInput) { in.Ingest = IngestSummary{RawReceived: true} }, "RAW_RECEIVED", "info"},
		{"stale after success", func(in *DiagnosisInput) { in.Ingest = IngestSummary{RawReceived: true, Parsed: true, Stale: true} }, "STALE", "warning"},
		{"single success", func(in *DiagnosisInput) { in.Ingest = IngestSummary{RawReceived: true, Parsed: true} }, "PARSED", "success"},
		{"continuous success", func(in *DiagnosisInput) {
			in.Ingest = IngestSummary{RawReceived: true, Parsed: true, ContinuouslyUpdating: true}
		}, "CONTINUOUS", "success"},
		{"credential device without public address", func(in *DiagnosisInput) { in.Profile = nil; in.UsesCredentials = true }, "ADDRESS_MISSING", "warning"},
		{"history is not current evidence", func(in *DiagnosisInput) { in.Ingest.PreviousParsedAt = 1700000000000 }, "PREVIOUSLY_PARSED", "info"},
		{"waiting", func(*DiagnosisInput) {}, "WAITING", "info"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := ready
			c.edit(&in)
			got := Diagnose(in)
			if got.Stage != c.stage || got.Tone != c.tone || got.Title == "" || got.NextAction == "" {
				t.Fatalf("Diagnose() = %+v, want stage %s tone %s", got, c.stage, c.tone)
			}
			if len(got.Checks) != 5 {
				t.Fatalf("checks = %d, want 5", len(got.Checks))
			}
		})
	}
}

func TestDiagnosisChecksReflectEachStage(t *testing.T) {
	got := Diagnose(DiagnosisInput{
		ProductEnabled: true, ProtocolValid: true, DeviceEnabled: true, UsesCredentials: true, AddressReady: true,
		Ingest: IngestSummary{ConfigurationSaved: true, RawReceived: true, ReceivedAt: 42, Parsed: true, ContinuouslyUpdating: true},
	})
	states := map[string]string{}
	for _, check := range got.Checks {
		states[check.Key] = check.State
	}
	for _, key := range []string{"configuration", "service", "raw", "parsed", "continuous"} {
		if states[key] != "passed" {
			t.Fatalf("%s = %q, want passed; checks %+v", key, states[key], got.Checks)
		}
	}
	if got.Checks[2].At != 42 {
		t.Fatalf("raw check should carry receivedAt: %+v", got.Checks[2])
	}
	failed := Diagnose(DiagnosisInput{ProductEnabled: true, ProtocolValid: true, DeviceEnabled: true, UsesCredentials: true, Ingest: IngestSummary{Stale: true, Parsed: true, RawReceived: true, ParseError: "x"}})
	for _, check := range failed.Checks {
		if (check.Key == "service" || check.Key == "parsed" || check.Key == "continuous") && check.State != "failed" {
			t.Fatalf("%s should fail: %+v", check.Key, failed.Checks)
		}
	}
}
