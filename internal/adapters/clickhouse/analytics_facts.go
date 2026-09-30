package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.AnalyticsFactStore = (*Repository)(nil)

// Business events, reception records and first availability receipts are read
// in PostgreSQL's fixed snapshot. ClickHouse values have their own read cutoff
// and are checked against those exact identities; its query time never claims
// all writes in another database were simultaneously visible.
func (r *Repository) AnalyticsFactsRead(ctx context.Context, tenant string, fn func(ports.AnalyticsFactReader) error) error {
	base, ok := r.Repository.(ports.AnalyticsFactStore)
	if !ok {
		return errors.New("analytics fact store unavailable")
	}
	return base.AnalyticsFactsRead(ctx, tenant, func(reader ports.AnalyticsFactReader) error {
		return fn(&clickhouseFactReader{AnalyticsFactReader: reader, repo: r, ctx: ctx, tenant: tenant})
	})
}

type clickhouseFactReader struct {
	ports.AnalyticsFactReader
	repo   *Repository
	ctx    context.Context
	tenant string
}

func (r *clickhouseFactReader) QueryMeasurementSeries(q model.FactQuery) (model.FactPage[model.MeasurementFact], error) {
	q.AvailabilitySource = "clickhouse_telemetry_ack"
	page, err := r.AnalyticsFactReader.QueryMeasurementSeries(q)
	if err != nil {
		return page, err
	}
	expected := []string{}
	for _, v := range page.Items {
		if telemetryMessage(model.StandardMessage{MessageType: v.MessageType}) {
			expected = append(expected, v.MessageID)
		}
	}
	slices.Sort(expected)
	expected = slices.Compact(expected)
	began := time.Now().UnixMilli()
	source := model.FactSourceCoverage{Source: "clickhouse_iot_telemetry", SourceVersion: fmt.Sprintf("query:%d", began), ReadAt: began, CoverageStart: q.Start, CoverageEnd: min(q.End, began), Complete: page.Source.Complete, Status: "AVAILABLE", CollectionStartedAt: page.Source.CollectionStartedAt, BackfillStatus: page.Source.BackfillStatus, HistoricalReconstructionQuality: page.Source.HistoricalReconstructionQuality, AvailableAtSource: "IMMUTABLE_STORAGE_ACK_OR_HISTORICAL_PROCESSED_AT", Limitations: []string{"INDEPENDENT_SOURCE_READ_CUTOFF"}}
	if len(expected) == 0 {
		page.AdditionalSources = append(page.AdditionalSources, source)
		page.Complete = page.Source.Complete && source.Complete
		return page, nil
	}
	quoteList := func(values []string) string {
		out := make([]string, len(values))
		for i, v := range values {
			out[i] = quote(v)
		}
		return strings.Join(out, ",")
	}
	sql := `SELECT message_id,any(toJSONString(properties)) AS properties FROM iot_telemetry WHERE tenant_id=` + quote(r.tenant) + ` AND device_id IN (` + quoteList(q.DeviceIDs) + `) AND message_id IN (` + quoteList(expected) + `) GROUP BY message_id FORMAT JSONEachRow`
	data, err := r.repo.query(r.ctx, sql, nil)
	if err != nil {
		return page, err
	}
	values := map[string]map[string]any{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row struct {
			MessageID  string `json:"message_id"`
			Properties string `json:"properties"`
		}
		if err = json.Unmarshal(line, &row); err != nil {
			return page, err
		}
		var props map[string]any
		if err = json.Unmarshal([]byte(row.Properties), &props); err != nil {
			return page, err
		}
		values[row.MessageID] = props
	}
	for i := range page.Items {
		v := &page.Items[i]
		if !telemetryMessage(model.StandardMessage{MessageType: v.MessageType}) {
			continue
		}
		value, exists := values[v.MessageID][v.Property]
		if !exists {
			source.Complete = false
			source.Status = "UNKNOWN"
			source.Limitations = appendUniqueFact(source.Limitations, "TELEMETRY_SOURCE_RECORD_MISSING")
			v.HistoricalReconstructionQuality = "SOURCE_RECORD_MISSING"
			v.Value = nil
			continue
		}
		v.Value = value
	}
	page.AdditionalSources = append(page.AdditionalSources, source)
	page.Complete = page.Source.Complete && source.Complete
	return page, nil
}
func appendUniqueFact(values []string, value string) []string {
	if !slices.Contains(values, value) {
		values = append(values, value)
	}
	return values
}
