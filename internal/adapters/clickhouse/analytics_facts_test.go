package clickhouse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type analyticsFactFixture struct {
	ports.AnalyticsFactReader
	page   model.FactPage[model.MeasurementFact]
	source string
}

func (f *analyticsFactFixture) QueryMeasurementSeries(q model.FactQuery) (model.FactPage[model.MeasurementFact], error) {
	f.source = q.AvailabilitySource
	out := f.page
	out.Items = append([]model.MeasurementFact(nil), f.page.Items...)
	return out, nil
}
func TestAnalyticsFactsClickHouseBatchParityAndMissingCoverage(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-values", true: "missing-source"}[missing], func(t *testing.T) {
			requested := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requested++
				q := r.URL.Query().Get("query")
				for _, piece := range []string{"tenant_id='t'", "device_id IN ('a')", "message_id IN ('m1','m2')"} {
					if !strings.Contains(q, piece) {
						t.Errorf("query missing explicit source scope: %s", q)
					}
				}
				rows := []map[string]string{{"message_id": "m1", "properties": `{"pressure":1}`}}
				if !missing {
					rows = append(rows, map[string]string{"message_id": "m2", "properties": `{"pressure":2}`})
				}
				for _, row := range rows {
					_ = json.NewEncoder(w).Encode(row)
				}
			}))
			defer server.Close()
			base := &analyticsFactFixture{page: model.FactPage[model.MeasurementFact]{Items: []model.MeasurementFact{{MessageID: "m1", DeviceID: "a", Property: "pressure", MessageType: model.PropertyReport, Value: float64(1)}, {MessageID: "m2", DeviceID: "a", Property: "pressure", MessageType: model.AlarmReport, Value: float64(2)}}, FactPageMeta: model.FactPageMeta{Complete: true, Source: model.FactSourceCoverage{Source: "standard_message", SourceVersion: "fixed-pg-snapshot", ReadAt: 123, Complete: true}}}}
			reader := &clickhouseFactReader{AnalyticsFactReader: base, repo: &Repository{base: server.URL, http: server.Client()}, ctx: context.Background(), tenant: "t"}
			page, err := reader.QueryMeasurementSeries(model.FactQuery{DeviceIDs: []string{"a"}, Start: 1, End: 100})
			if err != nil {
				t.Fatal(err)
			}
			if requested != 1 || base.source != "clickhouse_telemetry_ack" || len(page.AdditionalSources) != 1 || page.Source.SourceVersion != "fixed-pg-snapshot" || page.AdditionalSources[0].SourceVersion == page.Source.SourceVersion {
				t.Fatal("cross-database query did not preserve independent cutoffs", page)
			}
			if !missing {
				if !page.Complete || page.Items[0].Value != float64(1) || page.Items[1].Value != float64(2) {
					t.Fatal("ClickHouse/PostgreSQL parity lost", page)
				}
			} else {
				if page.Complete || page.Items[1].Value != nil || page.Items[1].HistoricalReconstructionQuality != "SOURCE_RECORD_MISSING" {
					t.Fatal("missing telemetry was presented as complete", page)
				}
			}
		})
	}
}
