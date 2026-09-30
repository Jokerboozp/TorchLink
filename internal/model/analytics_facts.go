package model

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// FactQuery always operates on an explicit authorized device set. Intervals are
// half-open; no empty set is interpreted as all devices.
type FactQuery struct {
	DeviceIDs          []string `json:"deviceIds"`
	Start              int64    `json:"start"`
	End                int64    `json:"end"`
	Properties         []string `json:"properties,omitempty"`
	TimeBasis          string   `json:"timeBasis,omitempty"`
	AvailabilitySource string   `json:"availabilitySource,omitempty"`
	Kind               string   `json:"kind,omitempty"`
	Limit              int      `json:"limit,omitempty"`
	Cursor             string   `json:"cursor,omitempty"`
	SourceVersion      string   `json:"sourceVersion,omitempty"`
}

func (q FactQuery) Validate() error {
	if q.Start < 0 || q.End <= q.Start {
		return errors.New("invalid fact interval")
	}
	if len(q.DeviceIDs) == 0 || len(q.DeviceIDs) > 2000 {
		return errors.New("explicit device scope of 1 to 2000 devices required")
	}
	for _, id := range q.DeviceIDs {
		if strings.TrimSpace(id) == "" {
			return errors.New("empty device identity")
		}
	}
	if q.Limit < 0 || q.Limit > 1000 {
		return errors.New("fact batch limit must not exceed 1000")
	}
	if q.AvailabilitySource != "" && !slices.Contains([]string{"postgres_standard_commit", "clickhouse_telemetry_ack"}, q.AvailabilitySource) {
		return errors.New("invalid availability source")
	}
	if q.TimeBasis != "" && !slices.Contains([]string{"EVENT", "RECEIVED", "AVAILABLE"}, q.TimeBasis) {
		return errors.New("invalid fact time basis")
	}
	return nil
}

type FactRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// Complete describes source coverage, not an assertion that every device
// reported. HasMore describes pagination and must be exhausted independently.
type FactSourceCoverage struct {
	Source                          string     `json:"source"`
	SourceVersion                   string     `json:"sourceVersion"`
	ReadAt                          int64      `json:"readAt"`
	CoverageStart                   int64      `json:"coverageStart"`
	CoverageEnd                     int64      `json:"coverageEnd"`
	Complete                        bool       `json:"complete"`
	Status                          string     `json:"status"` // AVAILABLE, UNKNOWN, EXPIRED, FORBIDDEN, QUERY_FAILED
	CollectionStartedAt             int64      `json:"collectionStartedAt"`
	BackfillRange                   *FactRange `json:"backfillRange,omitempty"`
	BackfillStatus                  string     `json:"backfillStatus"`
	AvailableAtSource               string     `json:"availableAtSource,omitempty"`
	HistoricalReconstructionQuality string     `json:"historicalReconstructionQuality"`
	Limitations                     []string   `json:"limitations,omitempty"`
}
type FactPageMeta struct {
	Complete          bool                 `json:"complete"`
	Source            FactSourceCoverage   `json:"source"`
	AdditionalSources []FactSourceCoverage `json:"additionalSources,omitempty"`
	Cursor            string               `json:"cursor,omitempty"`
	HasMore           bool                 `json:"hasMore"`
}
type FactPage[T any] struct {
	Items []T `json:"items"`
	FactPageMeta
}

type MeasurementFact struct {
	ID                              string      `json:"id"`
	TenantID                        string      `json:"tenantId"`
	DeviceID                        string      `json:"deviceId"`
	ProductID                       string      `json:"productId"`
	Property                        string      `json:"property"`
	Value                           any         `json:"value"`
	MessageID                       string      `json:"messageId"`
	RawMessageID                    string      `json:"rawMessageId"`
	MessageType                     MessageType `json:"messageType"`
	EventAt                         int64       `json:"eventAt"`
	ReceivedAt                      int64       `json:"receivedAt"`
	AvailableAt                     int64       `json:"availableAt"`
	AvailableAtSource               string      `json:"availableAtSource"`
	ParserVersion                   string      `json:"parserVersion,omitempty"`
	ProtocolVersion                 string      `json:"protocolVersion,omitempty"`
	PointTableVersion               string      `json:"pointTableVersion,omitempty"`
	Unit                            string      `json:"unit,omitempty"`
	HistoricalReconstructionQuality string      `json:"historicalReconstructionQuality"`
}
type RawParseOutcomeFact struct {
	RawMessageID         string `json:"rawMessageId"`
	DeviceID             string `json:"deviceId"`
	ProductID            string `json:"productId"`
	ReceivedAt           int64  `json:"receivedAt"`
	ArchivedAt           int64  `json:"archivedAt"`
	ParseAttemptedAt     int64  `json:"parseAttemptedAt"`
	ParseError           string `json:"parseError,omitempty"`
	Outcome              string `json:"outcome"` // ARCHIVED_NOT_ATTEMPTED, LAST_ATTEMPT_FAILED, STANDARD_SAVED, ATTEMPT_OUTCOME_UNKNOWN
	MessageID            string `json:"messageId,omitempty"`
	Protocol             string `json:"protocol,omitempty"`
	ProtocolVersion      string `json:"protocolVersion,omitempty"`
	PointTableVersion    string `json:"pointTableVersion,omitempty"`
	ArchiveBackend       string `json:"archiveBackend"`
	EvidenceAvailability string `json:"evidenceAvailability"`
}
type BusinessEventFact struct {
	SourceEventID   string          `json:"sourceEventId"`
	Source          string          `json:"source"`
	Actor           string          `json:"actor,omitempty"`
	ActorKind       string          `json:"actorKind"`
	ResourceID      string          `json:"resourceId"`
	ResourceVersion int64           `json:"resourceVersion"`
	DeviceID        string          `json:"deviceId"`
	Type            string          `json:"type"`
	OccurredAt      int64           `json:"occurredAt"`
	RecordedAt      int64           `json:"recordedAt"`
	Body            json.RawMessage `json:"body"`
}
type DeviceStateIntervalFact struct {
	DeviceID          string       `json:"deviceId"`
	Start             int64        `json:"start"`
	End               int64        `json:"end"`
	State             *DeviceState `json:"state,omitempty"`
	Quality           string       `json:"quality"` // KNOWN, UNKNOWN
	UnknownReason     string       `json:"unknownReason,omitempty"`
	SeedSourceEventID string       `json:"seedSourceEventId,omitempty"`
	SourceEventIDs    []string     `json:"sourceEventIds"`
}
type HandlingRecordFact struct {
	ID              string `json:"id"`
	DeviceID        string `json:"deviceId"`
	AlarmID         string `json:"alarmId,omitempty"`
	Source          string `json:"source"`
	Actor           string `json:"actor"`
	ResourceVersion int64  `json:"resourceVersion"`
	OccurredAt      int64  `json:"occurredAt"`
	RecordedAt      int64  `json:"recordedAt"`
	Content         string `json:"content"`
	CorrectsID      string `json:"correctsId,omitempty"`
}
type DependencyFact struct {
	ID              string `json:"id"`
	DeviceID        string `json:"deviceId"`
	Kind            string `json:"kind"`
	ResourceID      string `json:"resourceId"`
	SourceEventID   string `json:"sourceEventId"`
	ResourceVersion int64  `json:"resourceVersion"`
	EffectiveFrom   int64  `json:"effectiveFrom"`
	EffectiveTo     int64  `json:"effectiveTo"`
	RecordedAt      int64  `json:"recordedAt"`
	Quality         string `json:"quality"`
}
type ConfigurationFact struct {
	SourceEventID   string          `json:"sourceEventId"`
	Source          string          `json:"source"`
	ResourceID      string          `json:"resourceId"`
	ResourceVersion int64           `json:"resourceVersion"`
	DeviceID        string          `json:"deviceId,omitempty"`
	ProductID       string          `json:"productId,omitempty"`
	OccurredAt      int64           `json:"occurredAt"`
	RecordedAt      int64           `json:"recordedAt"`
	EffectiveTo     int64           `json:"effectiveTo,omitempty"`
	Body            json.RawMessage `json:"body"`
	InitialSnapshot bool            `json:"initialSnapshot"`
}
