package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.RuleLabInputStore = (*Repository)(nil)

func (r *Repository) RuleLabInputsRead(ctx context.Context, tenant string, fn func(ports.RuleLabInputReader) error) error {
	base, ok := r.Repository.(ports.RuleLabInputStore)
	if !ok {
		return errors.New("consistent rule lab standard reader unavailable")
	}
	return base.RuleLabInputsRead(ctx, tenant, func(reader ports.RuleLabInputReader) error {
		return fn(&ruleLabInputReader{base: reader, repo: r, ctx: ctx, tenant: tenant})
	})
}

type ruleLabInputReader struct {
	base   ports.RuleLabInputReader
	repo   *Repository
	ctx    context.Context
	tenant string
}

func (r *ruleLabInputReader) ListStandardInputs(q model.RuleLabInputQuery) (model.FactPage[model.RuleLabInput], error) {
	q.AvailabilitySource = "clickhouse_telemetry_ack"
	page, err := r.base.ListStandardInputs(q)
	if err != nil {
		return page, err
	}
	ids := []string{}
	for _, v := range page.Items {
		if telemetryMessage(v.Message) {
			ids = append(ids, v.ID)
		}
	}
	cutoff := time.Now().UnixMilli()
	source := model.FactSourceCoverage{Source: "clickhouse_iot_telemetry", SourceVersion: fmt.Sprintf("rulelab-query:%d", cutoff), ReadAt: cutoff, CoverageStart: page.Source.CoverageStart, CoverageEnd: min(page.Source.CoverageEnd, cutoff), CollectionStartedAt: page.Source.CollectionStartedAt, BackfillStatus: page.Source.BackfillStatus, HistoricalReconstructionQuality: page.Source.HistoricalReconstructionQuality, Complete: page.Source.Status == "AVAILABLE" && q.Start >= page.Source.CollectionStartedAt && q.End <= cutoff, Status: page.Source.Status, AvailableAtSource: "clickhouse_telemetry_ack", Limitations: []string{"INDEPENDENT_SOURCE_READ_CUTOFF"}}
	if len(ids) > 0 {
		quoted := make([]string, len(ids))
		for i, id := range ids {
			quoted[i] = quote(id)
		}
		devices := make([]string, len(q.DeviceIDs))
		for i, id := range q.DeviceIDs {
			devices[i] = quote(id)
		}
		data, err := r.repo.query(r.ctx, `SELECT message_id,any(toJSONString(properties)) AS properties FROM iot_telemetry WHERE tenant_id=`+quote(r.tenant)+` AND device_id IN (`+strings.Join(devices, ",")+`) AND message_id IN (`+strings.Join(quoted, ",")+`) GROUP BY message_id SETTINGS output_format_json_quote_64bit_integers=0 FORMAT JSONEachRow`, nil)
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
			var properties map[string]any
			if err = json.Unmarshal([]byte(row.Properties), &properties); err != nil {
				return page, err
			}
			values[row.MessageID] = properties
		}
		for i, v := range page.Items {
			if !telemetryMessage(v.Message) {
				continue
			}
			properties, ok := values[v.ID]
			if !ok {
				source.Complete = false
				source.Status = "UNKNOWN"
				source.Limitations = appendUniqueFact(source.Limitations, "TELEMETRY_SOURCE_RECORD_MISSING")
				page.Items[i].MetadataQuality = "SOURCE_RECORD_MISSING"
				page.Items[i].Message.Properties = nil
				continue
			}
			if len(properties) == 0 && len(v.Message.Properties) == 0 {
				properties = v.Message.Properties
			}
			page.Items[i].Message.Properties = properties
		}
	}
	page.AdditionalSources = append(page.AdditionalSources, source)
	page.Complete = page.Source.Complete && source.Complete
	return page, nil
}
