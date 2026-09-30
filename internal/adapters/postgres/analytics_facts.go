package postgres

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

//go:embed analytics_fact_schema.sql
var analyticsFactSchema string

// AnalyticsFactSchema is additive and is applied by the repository migration.
func AnalyticsFactSchema() string { return analyticsFactSchema }

var _ ports.AnalyticsFactStore = (*Repository)(nil)
var _ ports.MeasurementAvailabilityRecorder = (*Repository)(nil)

type analyticsFactReader struct {
	ctx             context.Context
	tx              pgx.Tx
	tenant, version string
	readAt          int64
}

func (r *Repository) AnalyticsFactsRead(ctx context.Context, tenant string, fn func(ports.AnalyticsFactReader) error) error {
	if strings.TrimSpace(tenant) == "" {
		return errors.New("fact tenant required")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	reader := &analyticsFactReader{ctx: ctx, tx: tx, tenant: tenant}
	if err = tx.QueryRow(ctx, `SELECT txid_current_snapshot()::text,`+nowMS).Scan(&reader.version, &reader.readAt); err != nil {
		return err
	}
	if err = fn(reader); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) RecordMeasurementAvailability(ctx context.Context, tenant, messageID, source string) error {
	if tenant == "" || messageID == "" || !slices.Contains([]string{"postgres_standard_commit", "clickhouse_telemetry_ack"}, source) {
		return errors.New("invalid measurement availability receipt")
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO measurement_availability(tenant_id,message_id,source,available_at,recorded_at) SELECT tenant_id,message_id,$3,`+nowMS+`,`+nowMS+` FROM standard_message WHERE tenant_id=$1 AND message_id=$2 ON CONFLICT DO NOTHING`, tenant, messageID, source)
	return err
}

type factCursor struct {
	Version string `json:"v"`
	Query   string `json:"q"`
	At      int64  `json:"a"`
	ID      string `json:"i"`
	Sub     string `json:"s"`
}

func factQueryHash(q model.FactQuery, source string) string {
	q.Cursor = ""
	q.SourceVersion = ""
	q.Limit = 0
	q.DeviceIDs = slices.Clone(q.DeviceIDs)
	slices.Sort(q.DeviceIDs)
	q.Properties = slices.Clone(q.Properties)
	slices.Sort(q.Properties)
	b, _ := json.Marshal(struct {
		Source string
		Query  model.FactQuery
	}{source, q})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func (r *analyticsFactReader) prepare(q model.FactQuery, source string) ([]any, factCursor, error) {
	if err := q.Validate(); err != nil {
		return nil, factCursor{}, err
	}
	if q.SourceVersion != "" && q.SourceVersion != r.version {
		return nil, factCursor{}, errors.New("fact source snapshot changed")
	}
	c := factCursor{Version: r.version, Query: factQueryHash(q, source), At: -1}
	if q.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(b, &c) != nil {
			return nil, c, errors.New("invalid fact cursor")
		}
		if c.Version != r.version || c.Query != factQueryHash(q, source) {
			return nil, c, errors.New("fact cursor scope or snapshot mismatch")
		}
	}
	limit := q.Limit
	if limit == 0 {
		limit = 500
	}
	properties := q.Properties
	if properties == nil {
		properties = []string{}
	}
	return []any{r.tenant, q.DeviceIDs, q.Start, q.End, limit + 1, c.At, c.ID, c.Sub, properties}, c, nil
}
func (r *analyticsFactReader) coverage(q model.FactQuery, source string) (model.FactSourceCoverage, error) {
	c := model.FactSourceCoverage{Source: source, SourceVersion: r.version, ReadAt: r.readAt, CoverageStart: q.Start, CoverageEnd: min(q.End, r.readAt), Status: "AVAILABLE", BackfillStatus: "NOT_STARTED", HistoricalReconstructionQuality: "PARTIAL"}
	var bs, be *int64
	err := r.tx.QueryRow(r.ctx, `SELECT collection_started_at,backfill_start,backfill_end,backfill_status,historical_quality FROM analytics_source_collection WHERE source=$1`, source).Scan(&c.CollectionStartedAt, &bs, &be, &c.BackfillStatus, &c.HistoricalReconstructionQuality)
	if errors.Is(err, pgx.ErrNoRows) {
		c.Status = "UNKNOWN"
		c.CoverageStart = c.CoverageEnd
		c.Limitations = []string{"SOURCE_COLLECTION_METADATA_MISSING"}
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if bs != nil && be != nil {
		c.BackfillRange = &model.FactRange{Start: *bs, End: *be}
	}
	c.Complete = q.Start >= c.CollectionStartedAt && q.End <= r.readAt
	if !c.Complete {
		c.CoverageStart = min(max(q.Start, c.CollectionStartedAt), c.CoverageEnd)
		c.Limitations = append(c.Limitations, "HISTORICAL_COLLECTION_OR_FUTURE_RANGE_NOT_COVERED")
	}
	if c.Complete {
		c.HistoricalReconstructionQuality = "RECORDED"
	}
	return c, nil
}
func finishFactPage[T any](items []T, q model.FactQuery, c factCursor, source model.FactSourceCoverage, key func(T) (int64, string, string)) model.FactPage[T] {
	limit := q.Limit
	if limit == 0 {
		limit = 500
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	p := model.FactPage[T]{Items: items, FactPageMeta: model.FactPageMeta{Complete: source.Complete, Source: source, HasMore: hasMore}}
	if hasMore && len(items) > 0 {
		c.At, c.ID, c.Sub = key(items[len(items)-1])
		b, _ := json.Marshal(c)
		p.Cursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return p
}
func (r *analyticsFactReader) QueryMeasurementSeries(q model.FactQuery) (model.FactPage[model.MeasurementFact], error) {
	args, c, err := r.prepare(q, "measurement")
	if err != nil {
		return model.FactPage[model.MeasurementFact]{}, err
	}
	source, err := r.coverage(q, "standard_message")
	if err != nil {
		return model.FactPage[model.MeasurementFact]{}, err
	}
	source.AvailableAtSource = "IMMUTABLE_STORAGE_ACK_OR_HISTORICAL_PROCESSED_AT"
	availabilitySource := q.AvailabilitySource
	if availabilitySource == "" {
		availabilitySource = "postgres_standard_commit"
	}
	members := q.Members
	if members == nil {
		members = []model.MeasurementIdentity{}
	}
	memberJSON, _ := json.Marshal(members)
	args = append(args, availabilitySource, memberJSON)
	if q.TimeBasis == "AVAILABLE" {
		var unknown bool
		err = r.tx.QueryRow(r.ctx, `SELECT EXISTS(SELECT 1 FROM standard_message s LEFT JOIN measurement_availability a ON a.tenant_id=s.tenant_id AND a.message_id=s.message_id AND a.source=$3 WHERE s.tenant_id=$1 AND s.device_id=ANY($2::text[]) AND s.processed_at=0 AND a.available_at IS NULL AND jsonb_typeof(s.properties)='object' AND s.properties<>'{}'::jsonb)`, r.tenant, q.DeviceIDs, availabilitySource).Scan(&unknown)
		if err != nil {
			return model.FactPage[model.MeasurementFact]{}, err
		}
		if unknown {
			source.Complete = false
			source.Limitations = appendUniqueFact(source.Limitations, "FIRST_AVAILABILITY_UNKNOWN")
		}
	}
	at := `s.ts`
	if q.TimeBasis == "RECEIVED" {
		at = `ri.received_at`
	}
	if q.TimeBasis == "AVAILABLE" {
		at = `COALESCE(a.available_at,NULLIF(s.processed_at,0))`
	}
	query := `SELECT s.message_id,s.raw_message_id,s.device_id,s.product_id,s.message_type,s.ts,COALESCE(ri.received_at,0),COALESCE(a.available_at,NULLIF(s.processed_at,0),0),CASE WHEN a.available_at IS NOT NULL THEN a.source WHEN s.processed_at>0 THEN 'historical_processed_at' ELSE 'UNKNOWN' END,s.body,p.key,p.value,COALESCE(raw.body,reservation.metadata,'{}'::jsonb),COALESCE(product_history.body,'{}'::jsonb),COALESCE(product_history.resource_version,0) FROM standard_message s LEFT JOIN raw_archive_index ri ON ri.tenant_id=s.tenant_id AND ri.message_id=s.raw_message_id LEFT JOIN measurement_availability a ON a.tenant_id=s.tenant_id AND a.message_id=s.message_id AND a.source=$10 LEFT JOIN raw_message_log raw ON raw.tenant_id=s.tenant_id AND raw.message_id=s.raw_message_id LEFT JOIN raw_ingest_reservation reservation ON reservation.tenant_id=s.tenant_id AND reservation.message_id=s.raw_message_id LEFT JOIN LATERAL(SELECT body,resource_version FROM analytics_configuration_event history WHERE history.tenant_id=s.tenant_id AND history.source='iot_product' AND history.resource_id=s.product_id AND history.occurred_at<=ri.received_at ORDER BY history.occurred_at DESC,history.resource_version DESC LIMIT 1)product_history ON true CROSS JOIN LATERAL jsonb_each(CASE WHEN jsonb_typeof(s.properties)='object' THEN s.properties ELSE '{}'::jsonb END) p WHERE s.tenant_id=$1 AND s.device_id=ANY($2::text[]) AND ` + at + ` >= $3 AND ` + at + ` < $4 AND (cardinality($9::text[])=0 OR p.key=ANY($9::text[])) AND ($11::jsonb='[]'::jsonb OR EXISTS(SELECT 1 FROM jsonb_array_elements($11::jsonb) member WHERE member->>'messageId'=s.message_id AND member->>'property'=p.key)) AND (` + at + `,s.message_id,p.key)>($6,$7,$8) ORDER BY ` + at + `,s.message_id,p.key LIMIT $5`
	rows, err := r.tx.Query(r.ctx, query, args...)
	if err != nil {
		return model.FactPage[model.MeasurementFact]{}, err
	}
	defer rows.Close()
	out := []model.MeasurementFact{}
	for rows.Next() {
		var v model.MeasurementFact
		var sb, value, rb, historicalBody []byte
		var historicalVersion int64
		if err = rows.Scan(&v.MessageID, &v.RawMessageID, &v.DeviceID, &v.ProductID, &v.MessageType, &v.EventAt, &v.ReceivedAt, &v.AvailableAt, &v.AvailableAtSource, &sb, &v.Property, &value, &rb, &historicalBody, &historicalVersion); err != nil {
			return model.FactPage[model.MeasurementFact]{}, err
		}
		v.ID = v.MessageID + ":" + v.Property
		v.TenantID = r.tenant
		if err = json.Unmarshal(value, &v.Value); err != nil {
			return model.FactPage[model.MeasurementFact]{}, err
		}
		var sm model.StandardMessage
		var raw model.RawMessage
		_ = json.Unmarshal(sb, &sm)
		_ = json.Unmarshal(rb, &raw)
		v.ParserVersion = sm.ParserVersion
		v.ProtocolVersion = raw.ProtocolVersion
		v.PointTableVersion = raw.PointTableVersion
		// The current thing model is not a historical unit. Only message tags
		// explicitly preserving the unit can supply it; otherwise it stays unknown.
		v.Unit = sm.Tags["unit:"+v.Property]
		v.ConfigurationVersion = sm.Tags["configurationVersion"]
		v.OperatingCondition = sm.Tags["operatingCondition"]
		if historicalVersion > 0 {
			var historicalProduct model.Product
			_ = json.Unmarshal(historicalBody, &historicalProduct)
			if historicalProduct.ThingModel != nil {
				for _, field := range historicalProduct.ThingModel.Properties {
					if field.Identifier == v.Property && v.Unit == "" {
						v.Unit = field.Unit
						break
					}
				}
			}
			if v.ConfigurationVersion == "" {
				v.ConfigurationVersion = fmt.Sprintf("product:%s:%d", v.ProductID, historicalVersion)
			}
		}
		v.HistoricalReconstructionQuality = "RECORDED"
		if v.AvailableAtSource == "historical_processed_at" {
			v.HistoricalReconstructionQuality = "CONSERVATIVE_PROCESSING_STAGE"
			source.HistoricalReconstructionQuality = "CONSERVATIVE_PROCESSING_STAGE"
			source.Limitations = appendUniqueFact(source.Limitations, "FIRST_AVAILABILITY_RECONSTRUCTED_AT_PROCESSING_STAGE")
		}
		if v.AvailableAt == 0 {
			v.HistoricalReconstructionQuality = "UNKNOWN"
			source.Complete = false
			source.Limitations = appendUniqueFact(source.Limitations, "FIRST_AVAILABILITY_UNKNOWN")
		}
		if v.ReceivedAt == 0 {
			source.Complete = false
			source.Limitations = appendUniqueFact(source.Limitations, "RECEPTION_TIME_UNKNOWN")
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return model.FactPage[model.MeasurementFact]{}, err
	}
	return finishFactPage(out, q, c, source, func(v model.MeasurementFact) (int64, string, string) {
		at := v.EventAt
		if q.TimeBasis == "RECEIVED" {
			at = v.ReceivedAt
		}
		if q.TimeBasis == "AVAILABLE" {
			at = v.AvailableAt
		}
		return at, v.MessageID, v.Property
	}), nil
}
func appendUniqueFact(values []string, value string) []string {
	if !slices.Contains(values, value) {
		values = append(values, value)
	}
	return values
}
func (r *analyticsFactReader) ListRawParseOutcomes(q model.FactQuery) (model.FactPage[model.RawParseOutcomeFact], error) {
	args, c, err := r.prepare(q, "raw")
	if err != nil {
		return model.FactPage[model.RawParseOutcomeFact]{}, err
	}
	source, err := r.coverage(q, "raw_archive_index")
	if err != nil {
		return model.FactPage[model.RawParseOutcomeFact]{}, err
	}
	rows, err := r.tx.Query(r.ctx, `SELECT ri.message_id,ri.device_id,ri.product_id,ri.received_at,ri.archived_at,ri.parse_attempted_at,ri.parse_error,COALESCE(s.message_id,''),COALESCE(s.standard_count,0),COALESCE(ri.protocol,''),ri.object_bucket,COALESCE(raw.body,reservation.metadata,'{}'::jsonb),raw.message_id IS NOT NULL FROM raw_archive_index ri LEFT JOIN LATERAL(SELECT count(*) standard_count,max(message_id) message_id FROM standard_message s WHERE s.tenant_id=ri.tenant_id AND s.raw_message_id=ri.message_id)s ON true LEFT JOIN raw_message_log raw ON raw.tenant_id=ri.tenant_id AND raw.message_id=ri.message_id LEFT JOIN raw_ingest_reservation reservation ON reservation.tenant_id=ri.tenant_id AND reservation.message_id=ri.message_id WHERE ri.tenant_id=$1 AND ri.device_id=ANY($2::text[]) AND ri.received_at>=$3 AND ri.received_at<$4 AND (ri.received_at,ri.message_id)>($6,$7) ORDER BY ri.received_at,ri.message_id LIMIT $5`, args[:7]...)
	if err != nil {
		return model.FactPage[model.RawParseOutcomeFact]{}, err
	}
	defer rows.Close()
	out := []model.RawParseOutcomeFact{}
	for rows.Next() {
		var v model.RawParseOutcomeFact
		var body []byte
		var stored bool
		if err = rows.Scan(&v.RawMessageID, &v.DeviceID, &v.ProductID, &v.ReceivedAt, &v.ArchivedAt, &v.ParseAttemptedAt, &v.ParseError, &v.MessageID, &v.SuccessfulStandardMessages, &v.Protocol, &v.ArchiveBackend, &body, &stored); err != nil {
			return model.FactPage[model.RawParseOutcomeFact]{}, err
		}
		var raw model.RawMessage
		_ = json.Unmarshal(body, &raw)
		v.ProtocolVersion = raw.ProtocolVersion
		v.PointTableVersion = raw.PointTableVersion
		v.EvidenceAvailability = "UNKNOWN"
		if stored {
			v.EvidenceAvailability = "AVAILABLE"
		} else if v.ArchiveBackend == "postgres" {
			v.EvidenceAvailability = "EXPIRED"
		}
		switch {
		case v.MessageID != "":
			v.Outcome = "STANDARD_SAVED"
		case v.ParseAttemptedAt == 0:
			v.Outcome = "ARCHIVED_NOT_ATTEMPTED"
		case v.ParseError != "":
			v.Outcome = "LAST_ATTEMPT_FAILED"
		default:
			v.Outcome = "ATTEMPT_OUTCOME_UNKNOWN"
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return model.FactPage[model.RawParseOutcomeFact]{}, err
	}
	return finishFactPage(out, q, c, source, func(v model.RawParseOutcomeFact) (int64, string, string) { return v.ReceivedAt, v.RawMessageID, "" }), nil
}
func (r *analyticsFactReader) businessEvents(q model.FactQuery, report bool) (model.FactPage[model.BusinessEventFact], error) {
	name := "lifecycle"
	types := []string{"ALARM_ACKNOWLEDGED", "ALARM_RECOVERED", "ALARM_CLOSED", "ALARM_STATUS_CHANGED"}
	if report {
		name = "reports"
		types = []string{"ALARM_CREATED", "ALARM_REPORTED"}
	}
	args, c, err := r.prepare(q, name)
	if err != nil {
		return model.FactPage[model.BusinessEventFact]{}, err
	}
	source, err := r.coverage(q, "duty_business_event")
	if err != nil {
		return model.FactPage[model.BusinessEventFact]{}, err
	}
	args = append(args[:7], types)
	rows, err := r.tx.Query(r.ctx, `SELECT id,event_type,device_id,actor_id,occurred_at,recorded_at,body FROM duty_business_event WHERE tenant_id=$1 AND device_id=ANY($2::text[]) AND occurred_at>=$3 AND occurred_at<$4 AND event_type=ANY($8::text[]) AND (occurred_at,id)>($6,$7) ORDER BY occurred_at,id LIMIT $5`, args...)
	if err != nil {
		return model.FactPage[model.BusinessEventFact]{}, err
	}
	defer rows.Close()
	out := []model.BusinessEventFact{}
	for rows.Next() {
		var v model.BusinessEventFact
		var body []byte
		if err = rows.Scan(&v.SourceEventID, &v.Type, &v.DeviceID, &v.Actor, &v.OccurredAt, &v.RecordedAt, &body); err != nil {
			return model.FactPage[model.BusinessEventFact]{}, err
		}
		var e model.DutyBusinessEvent
		if err = json.Unmarshal(body, &e); err != nil {
			return model.FactPage[model.BusinessEventFact]{}, err
		}
		v.Source = e.Source
		v.ResourceID = e.ResourceID
		v.ResourceVersion = e.ResourceVersion
		var alarm model.Alarm
		if json.Unmarshal(e.Body, &alarm) == nil && alarm.ID == e.ResourceID && alarm.DeviceID == e.DeviceID {
			var message model.StandardMessage
			encoded, _ := json.Marshal(alarm.Details["message"])
			if json.Unmarshal(encoded, &message) == nil && message.MessageID != "" && message.MessageID == alarm.TriggerID && message.DeviceID == alarm.DeviceID && (message.TenantID == "" || message.TenantID == r.tenant) && message.Timestamp > 0 {
				v.EventAt, v.MessageID, v.RawMessageID = message.Timestamp, message.MessageID, message.RawMessageID
			}
		}
		v.Body = alarmEventFactBody(e.Body)
		v.ActorKind = "AUTOMATIC"
		if v.Actor != "" {
			v.ActorKind = "HUMAN"
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return model.FactPage[model.BusinessEventFact]{}, err
	}
	return finishFactPage(out, q, c, source, func(v model.BusinessEventFact) (int64, string, string) { return v.OccurredAt, v.SourceEventID, "" }), nil
}

// Historical alarm snapshots can contain camera targets and arbitrary raw
// Details. The fact port exposes only the lifecycle facts it needs.
func alarmEventFactBody(body json.RawMessage) json.RawMessage {
	var values map[string]any
	if json.Unmarshal(body, &values) != nil {
		return json.RawMessage(`{}`)
	}
	out := map[string]any{}
	for _, key := range []string{"alarmId", "ruleId", "triggerId", "deviceId", "alarmType", "alarmLevel", "status", "source", "firstTriggeredAt", "lastTriggeredAt", "triggerCount", "recoveredAt", "ackedAt", "closedAt", "componentId", "componentName", "componentLocation"} {
		if value, ok := values[key]; ok {
			out[key] = value
		}
	}
	result, _ := json.Marshal(out)
	return result
}
func (r *analyticsFactReader) ListAlarmReportEvents(q model.FactQuery) (model.FactPage[model.BusinessEventFact], error) {
	return r.businessEvents(q, true)
}
func (r *analyticsFactReader) ListAlarmLifecycleEvents(q model.FactQuery) (model.FactPage[model.BusinessEventFact], error) {
	return r.businessEvents(q, false)
}

func (r *analyticsFactReader) ListDeviceStateIntervals(q model.FactQuery) (model.FactPage[model.DeviceStateIntervalFact], error) {
	args, c, err := r.prepare(q, "state")
	if err != nil {
		return model.FactPage[model.DeviceStateIntervalFact]{}, err
	}
	source, err := r.coverage(q, "device_state_event")
	if err != nil {
		return model.FactPage[model.DeviceStateIntervalFact]{}, err
	}
	args = append(args[:8], source.CollectionStartedAt)
	rows, err := r.tx.Query(r.ctx, `WITH events AS(
 SELECT id,device_id,occurred_at AS at,body->'body' AS state,true AS reliable FROM duty_business_event WHERE tenant_id=$1 AND device_id=ANY($2::text[]) AND body->>'source'='device' AND occurred_at<$4
 UNION ALL SELECT 'legacy:'||id::text,device_id,(extract(epoch FROM created_at)*1000)::bigint,body,false FROM device_state_event WHERE tenant_id=$1 AND device_id=ANY($2::text[]) AND created_at<to_timestamp(LEAST($4::bigint,$9::bigint)::double precision/1000)
 ),deduplicated AS(SELECT DISTINCT ON(device_id,at) * FROM events ORDER BY device_id,at,reliable DESC,id DESC),
 seeded AS(SELECT d.device_id,$3::bigint AS at,COALESCE(seed.id,'') AS id,seed.state,COALESCE(seed.reliable,false) AS reliable FROM unnest($2::text[]) d(device_id) LEFT JOIN LATERAL(SELECT * FROM deduplicated WHERE device_id=d.device_id AND at<=$3 ORDER BY at DESC,id DESC LIMIT 1)seed ON true),
 timeline AS(SELECT * FROM seeded UNION ALL SELECT device_id,at,id,state,reliable FROM deduplicated WHERE at>$3),
 intervals AS(SELECT *,LEAD(at,1,$4::bigint) OVER(PARTITION BY device_id ORDER BY at,id) AS until FROM timeline)
 SELECT device_id,at,until,id,state,reliable FROM intervals WHERE at<until AND (device_id,at,id)>($7,$6,$8) ORDER BY device_id,at,id LIMIT $5`, args...)
	if err != nil {
		return model.FactPage[model.DeviceStateIntervalFact]{}, err
	}
	defer rows.Close()
	out := []model.DeviceStateIntervalFact{}
	for rows.Next() {
		var v model.DeviceStateIntervalFact
		var id string
		var body []byte
		var reliable bool
		if err = rows.Scan(&v.DeviceID, &v.Start, &v.End, &id, &body, &reliable); err != nil {
			return model.FactPage[model.DeviceStateIntervalFact]{}, err
		}
		v.Quality = "KNOWN"
		v.SourceEventIDs = []string{}
		if id != "" {
			v.SourceEventIDs = []string{id}
			if v.Start == q.Start {
				v.SeedSourceEventID = id
			}
		}
		if len(body) > 0 {
			var state model.DeviceState
			if err = json.Unmarshal(body, &state); err != nil {
				return model.FactPage[model.DeviceStateIntervalFact]{}, err
			}
			v.State = &state
		}
		if !reliable {
			v.Quality = "UNKNOWN"
			v.UnknownReason = "LEGACY_EVENT_HISTORY_INCOMPLETE"
			if id == "" {
				v.UnknownReason = "WINDOW_START_STATE_MISSING"
			}
			source.Complete = false
			source.Limitations = appendUniqueFact(source.Limitations, v.UnknownReason)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return model.FactPage[model.DeviceStateIntervalFact]{}, err
	}
	return finishFactPage(out, q, c, source, func(v model.DeviceStateIntervalFact) (int64, string, string) {
		id := ""
		if len(v.SourceEventIDs) > 0 {
			id = v.SourceEventIDs[0]
		}
		return v.Start, v.DeviceID, id
	}), nil
}
func (r *analyticsFactReader) ListHandlingRecords(q model.FactQuery) (model.FactPage[model.HandlingRecordFact], error) {
	args, c, err := r.prepare(q, "handling")
	if err != nil {
		return model.FactPage[model.HandlingRecordFact]{}, err
	}
	source, err := r.coverage(q, "duty_record")
	if err != nil {
		return model.FactPage[model.HandlingRecordFact]{}, err
	}
	rows, err := r.tx.Query(r.ctx, `SELECT id,version,created_at,body FROM duty_record WHERE tenant_id=$1 AND body->>'deviceId'=ANY($2::text[]) AND (body->>'occurredAt')::bigint>=$3 AND (body->>'occurredAt')::bigint<$4 AND ((body->>'occurredAt')::bigint,id)>($6,$7) ORDER BY (body->>'occurredAt')::bigint,id LIMIT $5`, args[:7]...)
	if err != nil {
		return model.FactPage[model.HandlingRecordFact]{}, err
	}
	defer rows.Close()
	out := []model.HandlingRecordFact{}
	for rows.Next() {
		var v model.HandlingRecordFact
		var body []byte
		if err = rows.Scan(&v.ID, &v.ResourceVersion, &v.RecordedAt, &body); err != nil {
			return model.FactPage[model.HandlingRecordFact]{}, err
		}
		var record model.DutyRecord
		if err = json.Unmarshal(body, &record); err != nil {
			return model.FactPage[model.HandlingRecordFact]{}, err
		}
		v.DeviceID = record.DeviceID
		v.AlarmID = record.AlarmID
		v.Actor = record.AuthorID
		v.OccurredAt = record.OccurredAt
		v.Content = record.Content
		v.CorrectsID = record.CorrectsID
		v.Source = "duty_record"
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return model.FactPage[model.HandlingRecordFact]{}, err
	}
	return finishFactPage(out, q, c, source, func(v model.HandlingRecordFact) (int64, string, string) { return v.OccurredAt, v.ID, "" }), nil
}
func (r *analyticsFactReader) GetConfigurationHistory(q model.FactQuery) (model.FactPage[model.ConfigurationFact], error) {
	args, c, err := r.prepare(q, "configuration")
	if err != nil {
		return model.FactPage[model.ConfigurationFact]{}, err
	}
	source, err := r.coverage(q, "configuration_history")
	if err != nil {
		return model.FactPage[model.ConfigurationFact]{}, err
	}
	args = append(args[:7], q.Kind)
	rows, err := r.tx.Query(r.ctx, `WITH authorized_products AS(SELECT DISTINCT product_id FROM device_registry WHERE tenant_id=$1 AND id=ANY($2::text[])),allowed AS(SELECT * FROM analytics_configuration_event WHERE tenant_id=$1 AND (device_id=ANY($2::text[]) OR (device_id='' AND (product_id='' OR product_id IN(SELECT product_id FROM authorized_products)))) AND ($8='' OR source=$8)),versioned AS(SELECT *,LEAD(occurred_at) OVER(PARTITION BY source,resource_id ORDER BY occurred_at,resource_version) AS effective_to FROM allowed),seeds AS(SELECT DISTINCT ON(source,resource_id) * FROM versioned WHERE occurred_at<$3 ORDER BY source,resource_id,occurred_at DESC,seq DESC),selected AS(SELECT * FROM seeds UNION ALL SELECT * FROM versioned WHERE occurred_at>=$3 AND occurred_at<$4) SELECT seq::text,source,resource_id,resource_version,device_id,product_id,occurred_at,recorded_at,COALESCE(effective_to,0),body,initial_snapshot FROM selected WHERE (occurred_at,seq::text)>($6,$7) ORDER BY occurred_at,seq::text LIMIT $5`, args...)
	if err != nil {
		return model.FactPage[model.ConfigurationFact]{}, err
	}
	defer rows.Close()
	out := []model.ConfigurationFact{}
	for rows.Next() {
		var v model.ConfigurationFact
		if err = rows.Scan(&v.SourceEventID, &v.Source, &v.ResourceID, &v.ResourceVersion, &v.DeviceID, &v.ProductID, &v.OccurredAt, &v.RecordedAt, &v.EffectiveTo, &v.Body, &v.InitialSnapshot); err != nil {
			return model.FactPage[model.ConfigurationFact]{}, err
		}
		if v.Source == "device_registry" {
			var body map[string]any
			if err = json.Unmarshal(v.Body, &body); err != nil {
				return model.FactPage[model.ConfigurationFact]{}, err
			}
			if parent, _ := body["gatewayId"].(string); parent != "" && !slices.Contains(q.DeviceIDs, parent) {
				delete(body, "gatewayId")
			}
			v.Body, _ = json.Marshal(body)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return model.FactPage[model.ConfigurationFact]{}, err
	}
	return finishFactPage(out, q, c, source, func(v model.ConfigurationFact) (int64, string, string) { return v.OccurredAt, v.SourceEventID, "" }), nil
}
func (r *analyticsFactReader) GetDependencySnapshot(q model.FactQuery) (model.FactPage[model.DependencyFact], error) {
	args, c, err := r.prepare(q, "dependency")
	if err != nil {
		return model.FactPage[model.DependencyFact]{}, err
	}
	source, err := r.coverage(q, "configuration_history")
	if err != nil {
		return model.FactPage[model.DependencyFact]{}, err
	}
	var missingSeed bool
	err = r.tx.QueryRow(r.ctx, `SELECT EXISTS(SELECT 1 FROM unnest($2::text[]) device(id) WHERE NOT EXISTS(SELECT 1 FROM analytics_configuration_event e WHERE e.tenant_id=$1 AND e.source='device_registry' AND e.device_id=device.id AND e.occurred_at<=$3))`, r.tenant, q.DeviceIDs, q.Start).Scan(&missingSeed)
	if err != nil {
		return model.FactPage[model.DependencyFact]{}, err
	}
	if missingSeed {
		source.Complete = false
		source.Limitations = appendUniqueFact(source.Limitations, "DEPENDENCY_WINDOW_SEED_MISSING")
	}
	rows, err := r.tx.Query(r.ctx, `WITH versioned AS(
 SELECT *,LEAD(occurred_at) OVER(PARTITION BY source,resource_id ORDER BY occurred_at,resource_version) AS effective_to FROM analytics_configuration_event WHERE tenant_id=$1 AND device_id=ANY($2::text[]) AND source IN('device_registry','raw_collector')
 ),relations AS(SELECT e.seq::text AS source_event_id,e.device_id,e.resource_version,e.recorded_at,GREATEST(e.occurred_at,$3::bigint) AS since,LEAST(COALESCE(e.effective_to,$4::bigint),$4::bigint) AS until,v.kind,e.body->>v.field AS dependency_id FROM versioned e CROSS JOIN(VALUES('gatewayId','parent-device'),('connectorProfileId','access-profile'),('collectorId','collector'))v(field,kind) WHERE e.occurred_at<$4 AND (e.effective_to IS NULL OR e.effective_to>$3))
 SELECT source_event_id,device_id,resource_version,recorded_at,since,until,kind,dependency_id FROM relations WHERE since<until AND COALESCE(dependency_id,'')<>'' AND (kind<>'parent-device' OR dependency_id=ANY($2::text[])) AND (since,source_event_id,kind)>($6,$7,$8) ORDER BY since,source_event_id,kind LIMIT $5`, args[:8]...)
	if err != nil {
		return model.FactPage[model.DependencyFact]{}, err
	}
	defer rows.Close()
	out := []model.DependencyFact{}
	for rows.Next() {
		var v model.DependencyFact
		if err = rows.Scan(&v.SourceEventID, &v.DeviceID, &v.ResourceVersion, &v.RecordedAt, &v.EffectiveFrom, &v.EffectiveTo, &v.Kind, &v.ResourceID); err != nil {
			return model.FactPage[model.DependencyFact]{}, err
		}
		v.ID = v.SourceEventID + ":" + v.Kind
		v.Quality = "RECORDED"
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return model.FactPage[model.DependencyFact]{}, err
	}
	return finishFactPage(out, q, c, source, func(v model.DependencyFact) (int64, string, string) { return v.EffectiveFrom, v.SourceEventID, v.Kind }), nil
}
