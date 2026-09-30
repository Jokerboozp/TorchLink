package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.RuleLabInputStore = (*Repository)(nil)

func (r *Repository) RuleLabInputsRead(ctx context.Context, tenant string, fn func(ports.RuleLabInputReader) error) error {
	return r.AnalyticsFactsRead(ctx, tenant, func(reader ports.AnalyticsFactReader) error {
		base, ok := reader.(*analyticsFactReader)
		if !ok {
			return errors.New("consistent standard input reader unavailable")
		}
		return fn(&ruleLabInputReader{analyticsFactReader: base, availabilitySource: "postgres_standard_commit"})
	})
}

type ruleLabInputReader struct {
	*analyticsFactReader
	availabilitySource string
}

func (r *ruleLabInputReader) ListStandardInputs(q model.RuleLabInputQuery) (model.FactPage[model.RuleLabInput], error) {
	if !slices.Contains([]string{"EVENT", "RECEIVED"}, q.TimeBasis) || len(q.MessageIDs) > 1000 {
		return model.FactPage[model.RuleLabInput]{}, errors.New("invalid rule lab input query")
	}
	members := make([]model.MeasurementIdentity, len(q.MessageIDs))
	for i, id := range q.MessageIDs {
		if id == "" {
			return model.FactPage[model.RuleLabInput]{}, errors.New("empty standard input identity")
		}
		members[i] = model.MeasurementIdentity{MessageID: id}
	}
	sourceID := r.availabilitySource
	if q.AvailabilitySource != "" {
		sourceID = q.AvailabilitySource
	}
	fq := model.FactQuery{DeviceIDs: q.DeviceIDs, Start: q.Start, End: q.End, TimeBasis: q.TimeBasis, Members: members, Cursor: q.Cursor, Limit: q.Limit, AvailabilitySource: sourceID}
	args, cursor, err := r.prepare(fq, "rulelab-standard")
	if err != nil {
		return model.FactPage[model.RuleLabInput]{}, err
	}
	source, err := r.coverage(fq, "standard_message")
	if err != nil {
		return model.FactPage[model.RuleLabInput]{}, err
	}
	source.AvailableAtSource = "IMMUTABLE_STORAGE_ACK_OR_HISTORICAL_PROCESSED_AT"
	if q.TimeBasis == "RECEIVED" {
		var unknown bool
		if err = r.tx.QueryRow(r.ctx, `SELECT EXISTS(SELECT 1 FROM standard_message s LEFT JOIN raw_archive_index ri ON ri.tenant_id=s.tenant_id AND ri.message_id=s.raw_message_id WHERE s.tenant_id=$1 AND s.device_id=ANY($2::text[]) AND COALESCE(ri.received_at,0)=0)`, r.tenant, q.DeviceIDs).Scan(&unknown); err != nil {
			return model.FactPage[model.RuleLabInput]{}, err
		}
		if unknown {
			source.Complete = false
			source.Limitations = appendUniqueFact(source.Limitations, "RECEPTION_TIME_UNKNOWN_MEMBERS_CANNOT_BE_PLACED")
		}
	}
	args = append(args[:7], sourceID, q.MessageIDs)
	at := `s.ts`
	if q.TimeBasis == "RECEIVED" {
		at = `COALESCE(ri.received_at,0)`
	}
	rows, err := r.tx.Query(r.ctx, `SELECT s.message_id,s.body,COALESCE(ri.received_at,0),COALESCE(a.available_at,NULLIF(s.processed_at,0),0),CASE WHEN a.available_at IS NOT NULL THEN a.source WHEN s.processed_at>0 THEN 'historical_processed_at' ELSE 'UNKNOWN' END,COALESCE(raw.body,reservation.metadata,'{}'::jsonb),COALESCE(product_history.body,'{}'::jsonb),COALESCE(product_history.resource_version,0) FROM standard_message s LEFT JOIN raw_archive_index ri ON ri.tenant_id=s.tenant_id AND ri.message_id=s.raw_message_id LEFT JOIN measurement_availability a ON a.tenant_id=s.tenant_id AND a.message_id=s.message_id AND a.source=CASE WHEN s.body->>'messageType' IN ('PROPERTY_REPORT','ALARM_REPORT') THEN $8 ELSE 'postgres_standard_commit' END LEFT JOIN raw_message_log raw ON raw.tenant_id=s.tenant_id AND raw.message_id=s.raw_message_id LEFT JOIN raw_ingest_reservation reservation ON reservation.tenant_id=s.tenant_id AND reservation.message_id=s.raw_message_id LEFT JOIN LATERAL(SELECT body,resource_version FROM analytics_configuration_event history WHERE history.tenant_id=s.tenant_id AND history.source='iot_product' AND history.resource_id=s.product_id AND history.occurred_at<=ri.received_at ORDER BY history.occurred_at DESC,history.resource_version DESC LIMIT 1)product_history ON true WHERE s.tenant_id=$1 AND s.device_id=ANY($2::text[]) AND `+at+`>=$3 AND `+at+`<$4 AND (COALESCE(cardinality($9::text[]),0)=0 OR s.message_id=ANY($9::text[])) AND (`+at+`,s.message_id)>($6,$7) ORDER BY `+at+`,s.message_id LIMIT $5`, args...)
	if err != nil {
		return model.FactPage[model.RuleLabInput]{}, err
	}
	defer rows.Close()
	items := []model.RuleLabInput{}
	for rows.Next() {
		var v model.RuleLabInput
		var body, rawBody, productBody []byte
		var productVersion int64
		if err = rows.Scan(&v.ID, &body, &v.ReceivedAt, &v.AvailableAt, &v.AvailableAtSource, &rawBody, &productBody, &productVersion); err != nil {
			return model.FactPage[model.RuleLabInput]{}, err
		}
		if err = json.Unmarshal(body, &v.Message); err != nil {
			return model.FactPage[model.RuleLabInput]{}, err
		}
		if v.Message.TenantID != r.tenant || !slices.Contains(q.DeviceIDs, v.Message.DeviceID) || v.Message.MessageID != v.ID {
			return model.FactPage[model.RuleLabInput]{}, errors.New("stored standard input identity mismatch")
		}
		var raw model.RawMessage
		_ = json.Unmarshal(rawBody, &raw)
		v.ProtocolVersion = raw.ProtocolVersion
		v.PointTableVersion = raw.PointTableVersion
		v.ConfigurationVersion = v.Message.Tags["configurationVersion"]
		v.Units = map[string]string{}
		for key, unit := range v.Message.Tags {
			if strings.HasPrefix(key, "unit:") {
				v.Units[strings.TrimPrefix(key, "unit:")] = unit
			}
		}
		if productVersion > 0 {
			var product model.Product
			_ = json.Unmarshal(productBody, &product)
			if product.ThingModel != nil {
				for _, field := range product.ThingModel.Properties {
					if v.Units[field.Identifier] == "" {
						v.Units[field.Identifier] = field.Unit
					}
				}
			}
			if v.ConfigurationVersion == "" {
				v.ConfigurationVersion = fmt.Sprintf("product:%s:%d", v.Message.ProductID, productVersion)
			}
		}
		v.MetadataQuality = "RECORDED"
		v.Traces = []model.RuleEvaluationTrace{}
		if v.ReceivedAt == 0 {
			v.MetadataQuality = "UNKNOWN"
			source.Complete = false
			source.Limitations = appendUniqueFact(source.Limitations, "RECEPTION_TIME_UNKNOWN")
		}
		if v.AvailableAt == 0 {
			v.MetadataQuality = "UNKNOWN"
			source.Complete = false
			source.Limitations = appendUniqueFact(source.Limitations, "FIRST_AVAILABILITY_UNKNOWN")
		}
		if v.AvailableAtSource == "historical_processed_at" {
			v.MetadataQuality = "CONSERVATIVE_PROCESSING_STAGE"
			source.Limitations = appendUniqueFact(source.Limitations, "FIRST_AVAILABILITY_RECONSTRUCTED_AT_PROCESSING_STAGE")
		}
		if v.ProtocolVersion == "" || v.PointTableVersion == "" || v.ConfigurationVersion == "" {
			v.MetadataQuality = "PARTIAL_HISTORICAL_METADATA"
			source.Limitations = appendUniqueFact(source.Limitations, "HISTORICAL_PROTOCOL_OR_CONFIGURATION_METADATA_MISSING")
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		return model.FactPage[model.RuleLabInput]{}, err
	}
	return finishFactPage(items, fq, cursor, source, func(v model.RuleLabInput) (int64, string, string) {
		at := v.Message.Timestamp
		if q.TimeBasis == "RECEIVED" {
			at = v.ReceivedAt
		}
		return at, v.ID, ""
	}), nil
}
