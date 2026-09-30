package clickhouse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"iot-platform/internal/model"
)

type ruleLabFixture struct {
	page   model.FactPage[model.RuleLabInput]
	source string
}

func (f *ruleLabFixture) ListStandardInputs(q model.RuleLabInputQuery) (model.FactPage[model.RuleLabInput], error) {
	f.source = q.AvailabilitySource
	p := f.page
	p.Items = append([]model.RuleLabInput(nil), p.Items...)
	return p, nil
}
func TestRuleLabClickHouseWholeMessageSourceCoverageAndMissingValues(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-source", true: "missing-telemetry"}[missing], func(t *testing.T) {
			requested := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requested++
				query := r.URL.Query().Get("query")
				for _, scope := range []string{"tenant_id='t'", "device_id IN ('a')", "message_id IN ('m1','m2')"} {
					if !strings.Contains(query, scope) {
						t.Error("unbounded telemetry source query")
					}
				}
				json.NewEncoder(w).Encode(map[string]string{"message_id": "m1", "properties": `{"pressure":100}`})
				if !missing {
					json.NewEncoder(w).Encode(map[string]string{"message_id": "m2", "properties": `{}`})
				}
			}))
			defer server.Close()
			base := &ruleLabFixture{page: model.FactPage[model.RuleLabInput]{Items: []model.RuleLabInput{{ID: "m1", Message: model.StandardMessage{MessageID: "m1", RawMessageID: "shared-raw", TenantID: "t", DeviceID: "a", MessageType: model.PropertyReport, Properties: map[string]any{"pressure": 999}}}, {ID: "m2", Message: model.StandardMessage{MessageID: "m2", RawMessageID: "shared-raw", TenantID: "t", DeviceID: "a", MessageType: model.PropertyReport}}, {ID: "m3", Message: model.StandardMessage{MessageID: "m3", RawMessageID: "shared-raw", TenantID: "t", DeviceID: "a", MessageType: model.EventReport, Event: map[string]any{"dateText": "2026-10-01T00:00:00Z"}}}}, FactPageMeta: model.FactPageMeta{Complete: true, Source: model.FactSourceCoverage{Source: "standard_message", SourceVersion: "repeatable-read", ReadAt: 123, CoverageStart: 1, CoverageEnd: 100, CollectionStartedAt: 1, Complete: true, Status: "AVAILABLE"}}}}
			reader := &ruleLabInputReader{base: base, repo: &Repository{base: server.URL, http: server.Client()}, ctx: context.Background(), tenant: "t"}
			page, err := reader.ListStandardInputs(model.RuleLabInputQuery{DeviceIDs: []string{"a"}, Start: 1, End: 100, TimeBasis: "EVENT", Limit: 10})
			if err != nil || requested != 1 || base.source != "clickhouse_telemetry_ack" || len(page.Items) != 3 || len(page.AdditionalSources) != 1 {
				t.Fatal(page, err)
			}
			if page.Items[0].Message.Properties["pressure"] != float64(100) || page.Items[2].Message.Event["dateText"] != "2026-10-01T00:00:00Z" || page.Items[2].Message.RawMessageID != "shared-raw" {
				t.Fatal("whole source contents flattened")
			}
			if missing {
				if page.Complete || page.AdditionalSources[0].Complete || page.Items[1].MetadataQuality != "SOURCE_RECORD_MISSING" || page.Items[1].Message.Properties != nil {
					t.Fatal("missing source was replaced with PG values", page)
				}
			} else if !page.Complete || page.AdditionalSources[0].SourceVersion == page.Source.SourceVersion {
				t.Fatal("independent source coverage was lost", page)
			}
		})
	}
}
