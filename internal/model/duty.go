package model

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	DutyAttachmentKind    = "attachment"
	DutyStationKind       = "station"
	DutyTeamKind          = "team"
	DutyShiftTemplateKind = "shift-template"
	DutyRosterKind        = "roster"
	DutyRunKind           = "run"
	DutyRecordKind        = "record"
	DutyItemKind          = "item"
	DutyItemEventKind     = "item-event"
	DutyHandoverKind      = "handover"
	DutyRevisionKind      = "revision"
	DutyAIJobKind         = "ai-job"
	DutyNotificationKind  = "notification"
)

var ErrDutyConflict = errors.New("duty version conflict")
var ErrDutyReadOnly = errors.New("duty transaction is read only")

// DutyDocument carries storage metadata; Body is one of the typed objects below.
// All duty timestamps are Unix milliseconds and time windows are [start,end).
type DutyDocument struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenantId"`
	Kind      string          `json:"kind"`
	Version   int64           `json:"version"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
	Body      json.RawMessage `json:"body"`
}

func NewDutyDocument(kind, id string, body any) DutyDocument {
	b, _ := json.Marshal(body)
	return DutyDocument{Kind: kind, ID: id, Body: b}
}
func DutyBody[T any](doc DutyDocument) (v T, err error) { err = json.Unmarshal(doc.Body, &v); return }

type DutyFilter struct {
	Kind       string   `json:"kind,omitempty"`
	StationID  string   `json:"stationId,omitempty"`
	RunID      string   `json:"runId,omitempty"`
	HandoverID string   `json:"handoverId,omitempty"`
	Status     string   `json:"status,omitempty"`
	UserID     string   `json:"userId,omitempty"`
	DeviceIDs  []string `json:"deviceIds,omitempty"`
	Start      int64    `json:"start,omitempty"`
	End        int64    `json:"end,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Offset     int      `json:"offset,omitempty"`
}
type DutyStation struct {
	Name            string   `json:"name"`
	SupervisorID    string   `json:"supervisorId"`
	DeviceIDs       []string `json:"deviceIds"`
	ScopeVersion    int64    `json:"scopeVersion"`
	RequiredPeople  int      `json:"requiredPeople"`
	ReminderMinutes int      `json:"reminderMinutes"`
	Enabled         bool     `json:"enabled"`
	Timezone        string   `json:"timezone"`
}
type DutyTeam struct {
	Name      string   `json:"name"`
	MemberIDs []string `json:"memberIds"`
	LeaderID  string   `json:"leaderId"`
}
type DutyShiftTemplate struct {
	Name         string `json:"name"`
	StartTime    string `json:"startTime"`
	EndTime      string `json:"endTime"`
	EndDayOffset int    `json:"endDayOffset"`
}
type DutyRoster struct {
	DeviceIDs     []string           `json:"deviceIds"`
	ScopeVersion  int64              `json:"scopeVersion"`
	StationID     string             `json:"stationId"`
	TemplateID    string             `json:"templateId,omitempty"`
	TeamID        string             `json:"teamId,omitempty"`
	StartAt       int64              `json:"startAt"`
	EndAt         int64              `json:"endAt"`
	MemberIDs     []string           `json:"memberIds"`
	LeaderID      string             `json:"leaderId"`
	Status        string             `json:"status"`
	Reason        string             `json:"reason,omitempty"`
	Source        string             `json:"source,omitempty"`
	SourceID      string             `json:"sourceId,omitempty"`
	ImportBatchID string             `json:"importBatchId,omitempty"`
	History       []DutyRosterChange `json:"history,omitempty"`
}
type DutyRosterChange struct {
	At       int64           `json:"at"`
	ActorID  string          `json:"actorId"`
	Reason   string          `json:"reason"`
	Previous json.RawMessage `json:"previous"`
}
type DutyAttendance struct {
	UserID    string `json:"userId"`
	ArrivedAt int64  `json:"arrivedAt"`
	LeftAt    int64  `json:"leftAt,omitempty"`
	Reason    string `json:"reason,omitempty"`
}
type DutyRun struct {
	Baseline           *DutySnapshot    `json:"baseline,omitempty"`
	RosterID           string           `json:"rosterId"`
	StationID          string           `json:"stationId"`
	MemberIDs          []string         `json:"memberIds"`
	LeaderID           string           `json:"leaderId"`
	DeviceIDs          []string         `json:"deviceIds"`
	ScopeVersion       int64            `json:"scopeVersion"`
	Status             string           `json:"status"`
	StartedAt          int64            `json:"startedAt"`
	EndedAt            int64            `json:"endedAt,omitempty"`
	EndReason          string           `json:"endReason,omitempty"`
	Attendance         []DutyAttendance `json:"attendance"`
	PreviousRunID      string           `json:"previousRunId,omitempty"`
	IncomingHandoverID string           `json:"incomingHandoverId,omitempty"`
}
type DutyAttachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ObjectKey   string `json:"objectKey"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}
type DutyRecord struct {
	StationID        string           `json:"stationId"`
	RunID            string           `json:"runId"`
	DeviceID         string           `json:"deviceId,omitempty"`
	AlarmID          string           `json:"alarmId,omitempty"`
	ItemID           string           `json:"itemId,omitempty"`
	AuthorID         string           `json:"authorId"`
	Content          string           `json:"content"`
	OccurredAt       int64            `json:"occurredAt"`
	Attachments      []DutyAttachment `json:"attachments,omitempty"`
	CorrectsID       string           `json:"correctsId,omitempty"`
	CorrectionReason string           `json:"correctionReason,omitempty"`
}
type DutyItem struct {
	StationID   string `json:"stationId"`
	RunID       string `json:"runId"`
	Title       string `json:"title"`
	DeviceID    string `json:"deviceId,omitempty"`
	AlarmID     string `json:"alarmId,omitempty"`
	SourceID    string `json:"sourceId,omitempty"`
	OwnerID     string `json:"ownerId"`
	Status      string `json:"status"`
	NextAction  string `json:"nextAction"`
	DueAt       int64  `json:"dueAt,omitempty"`
	Result      string `json:"result,omitempty"`
	Reason      string `json:"reason,omitempty"`
	CreatedBy   string `json:"createdBy"`
	CompletedAt int64  `json:"completedAt,omitempty"`
}
type DutyItemEvent struct {
	StationID       string `json:"stationId"`
	RunID           string `json:"runId"`
	ItemID          string `json:"itemId"`
	Type            string `json:"type"`
	ActorID         string `json:"actorId"`
	PreviousOwnerID string `json:"previousOwnerId,omitempty"`
	OwnerID         string `json:"ownerId,omitempty"`
	Content         string `json:"content"`
	OccurredAt      int64  `json:"occurredAt"`
}
type DutyConfirmation struct {
	UserID         string `json:"userId"`
	At             int64  `json:"at"`
	RevisionID     string `json:"revisionId"`
	SnapshotHash   string `json:"snapshotHash"`
	IdempotencyKey string `json:"idempotencyKey"`
	Note           string `json:"note,omitempty"`
}
type DutyHandover struct {
	DeviceIDs          []string                `json:"deviceIds"`
	AcceptanceSnapshot *DutySnapshot           `json:"acceptanceSnapshot,omitempty"`
	StationID          string                  `json:"stationId"`
	RunID              string                  `json:"runId"`
	NextRosterID       string                  `json:"nextRosterId"`
	NextRunID          string                  `json:"nextRunId,omitempty"`
	Status             string                  `json:"status"`
	CurrentRevisionID  string                  `json:"currentRevisionId"`
	RevisionNumber     int                     `json:"revisionNumber"`
	Submission         *DutyConfirmation       `json:"submission,omitempty"`
	Acceptance         *DutyConfirmation       `json:"acceptance,omitempty"`
	ReturnReason       string                  `json:"returnReason,omitempty"`
	VoidReason         string                  `json:"voidReason,omitempty"`
	Amendments         []DutyHandoverAmendment `json:"amendments,omitempty"`
}
type DutyHandoverAmendment struct {
	ID       string   `json:"id"`
	ActorID  string   `json:"actorId"`
	At       int64    `json:"at"`
	Content  string   `json:"content"`
	EventIDs []string `json:"eventIds,omitempty"`
}
type DutyEvidence struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	DeviceID string `json:"deviceId,omitempty"`
	Label    string `json:"label"`
	Content  string `json:"content,omitempty"`
	At       int64  `json:"at,omitempty"`
}
type DutyAIStatement struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidenceIds"`
}
type DutyAIResult struct {
	Summary     string            `json:"summary"`
	Highlights  []DutyAIStatement `json:"highlights"`
	Suggestions []DutyAIStatement `json:"suggestions"`
	Missing     []DutyAIStatement `json:"missing"`
	InputCount  int               `json:"inputCount"`
	TotalCount  int               `json:"totalCount"`
	Truncated   bool              `json:"truncated"`
}
type DutyStatistics struct {
	NewAlarms    int `json:"newAlarms"`
	Reports      int `json:"reports"`
	Acknowledged int `json:"acknowledged"`
	Recovered    int `json:"recovered"`
	Closed       int `json:"closed"`
	Offline      int `json:"offline"`
	Online       int `json:"online"`
}
type DutySnapshot struct {
	CutoffAt  int64           `json:"cutoffAt"`
	EventSeq  int64           `json:"eventSeq"`
	DeviceIDs []string        `json:"deviceIds"`
	Devices   []ManagedDevice `json:"devices"`
	States    []DeviceState   `json:"states"`
	Alarms    []Alarm         `json:"alarms"`
}
type DutyHandoverRevision struct {
	Frozen       bool                `json:"frozen"`
	StationID    string              `json:"stationId"`
	RunID        string              `json:"runId"`
	HandoverID   string              `json:"handoverId"`
	Number       int                 `json:"number"`
	AuthorID     string              `json:"authorId"`
	StartAt      int64               `json:"startAt"`
	Snapshot     DutySnapshot        `json:"snapshot"`
	SnapshotHash string              `json:"snapshotHash"`
	Statistics   DutyStatistics      `json:"statistics"`
	Events       []DutyBusinessEvent `json:"events"`
	EventTotal   int                 `json:"eventTotal"`
	Records      []DutyDocument      `json:"records"`
	Items        []DutyDocument      `json:"items"`
	Evidence     []DutyEvidence      `json:"evidence"`
	HumanNotes   string              `json:"humanNotes"`
	AI           *DutyAIResult       `json:"ai,omitempty"`
	AIJobID      string              `json:"aiJobId,omitempty"`
}
type DutyAIJob struct {
	SessionVersion int64         `json:"sessionVersion"`
	AccessVersion  int64         `json:"accessVersion"`
	ManagedUser    bool          `json:"managedUser"`
	DeviceIDs      []string      `json:"deviceIds"`
	Permissions    []string      `json:"permissions"`
	UseKnowledge   bool          `json:"useKnowledge"`
	InputLimit     int           `json:"inputLimit"`
	HarnessRunID   string        `json:"harnessRunId,omitempty"`
	StationID      string        `json:"stationId"`
	RunID          string        `json:"runId"`
	HandoverID     string        `json:"handoverId"`
	RevisionID     string        `json:"revisionId"`
	RequesterID    string        `json:"requesterId"`
	Status         string        `json:"status"`
	Stage          string        `json:"stage"`
	WorkflowID     string        `json:"workflowId"`
	Model          string        `json:"model,omitempty"`
	Error          string        `json:"error,omitempty"`
	LeaseOwner     string        `json:"leaseOwner,omitempty"`
	LeaseUntil     int64         `json:"leaseUntil,omitempty"`
	Attempt        int           `json:"attempt"`
	StartedAt      int64         `json:"startedAt,omitempty"`
	FinishedAt     int64         `json:"finishedAt,omitempty"`
	Result         *DutyAIResult `json:"result,omitempty"`
}
type DutyNotification struct {
	StationID  string `json:"stationId"`
	RunID      string `json:"runId,omitempty"`
	UserID     string `json:"userId"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	ResourceID string `json:"resourceId"`
	ReadAt     int64  `json:"readAt,omitempty"`
}
type DutyBusinessEvent struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenantId"`
	Seq             int64           `json:"seq"`
	Type            string          `json:"type"`
	Source          string          `json:"source"`
	ResourceID      string          `json:"resourceId"`
	ResourceVersion int64           `json:"resourceVersion"`
	StationID       string          `json:"stationId,omitempty"`
	RunID           string          `json:"runId,omitempty"`
	DeviceID        string          `json:"deviceId,omitempty"`
	AlarmID         string          `json:"alarmId,omitempty"`
	ActorID         string          `json:"actorId,omitempty"`
	OccurredAt      int64           `json:"occurredAt"`
	RecordedAt      int64           `json:"recordedAt"`
	Body            json.RawMessage `json:"body"`
}
type dutyActorKey struct{}

func WithDutyActor(ctx context.Context, actorID string) context.Context {
	return context.WithValue(ctx, dutyActorKey{}, actorID)
}
func DutyActor(ctx context.Context) string { v, _ := ctx.Value(dutyActorKey{}).(string); return v }

// DutyAttachmentRecord is immutable; uploaded object keys are server-owned.
type DutyAttachmentRecord struct {
	StationID  string         `json:"stationId"`
	RunID      string         `json:"runId"`
	AuthorID   string         `json:"authorId"`
	Attachment DutyAttachment `json:"attachment"`
}

func dutyEventID(tenant, resource, kind string, version int64) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%d", tenant, resource, kind, version))))
}

// DutyAlarmEvents is the reliable ledger companion to the committed alarm row.
func DutyAlarmEvents(ctx context.Context, old, next Alarm) []DutyBusinessEvent {
	types := []struct {
		kind string
		at   int64
	}{}
	if old.ID == "" {
		types = append(types, struct {
			kind string
			at   int64
		}{"ALARM_CREATED", next.FirstTriggeredAt})
	} else if next.TriggerCount > old.TriggerCount {
		types = append(types, struct {
			kind string
			at   int64
		}{"ALARM_REPORTED", next.LastTriggeredAt})
	}
	if next.AckedAt > old.AckedAt {
		types = append(types, struct {
			kind string
			at   int64
		}{"ALARM_ACKNOWLEDGED", next.AckedAt})
	}
	if next.RecoveredAt > old.RecoveredAt {
		types = append(types, struct {
			kind string
			at   int64
		}{"ALARM_RECOVERED", next.RecoveredAt})
	}
	if next.ClosedAt > old.ClosedAt {
		types = append(types, struct {
			kind string
			at   int64
		}{"ALARM_CLOSED", next.ClosedAt})
	}
	if len(types) == 0 && old.ID != "" && old.Status != next.Status {
		types = append(types, struct {
			kind string
			at   int64
		}{"ALARM_STATUS_CHANGED", time.Now().UnixMilli()})
	}
	out := []DutyBusinessEvent{}
	body, _ := json.Marshal(next)
	now := time.Now().UnixMilli()
	for _, v := range types {
		at := v.at
		if at == 0 {
			at = now
		}
		out = append(out, DutyBusinessEvent{ID: dutyEventID(next.TenantID, next.ID, v.kind, next.Version), TenantID: next.TenantID, Type: v.kind, Source: "alarm", ResourceID: next.ID, ResourceVersion: next.Version, DeviceID: next.DeviceID, AlarmID: next.ID, ActorID: DutyActor(ctx), OccurredAt: at, RecordedAt: now, Body: body})
	}
	return out
}
func DutyDeviceEvent(ctx context.Context, old, next DeviceState) *DutyBusinessEvent {
	if old.BusinessStatus == next.BusinessStatus && old.ConnectionStatus == next.ConnectionStatus {
		return nil
	}
	kind := "DEVICE_STATUS_CHANGED"
	at := next.LastSeenAt
	if next.BusinessStatus == "OFFLINE" || next.ConnectionStatus == "OFFLINE" {
		kind = "DEVICE_OFFLINE"
		at = next.OfflineAt
		if at == 0 {
			at = next.LastDisconnectAt
		}
	} else if old.BusinessStatus == "OFFLINE" || old.ConnectionStatus == "OFFLINE" {
		kind = "DEVICE_ONLINE"
		at = next.LastConnectAt
	}
	if at == 0 {
		at = time.Now().UnixMilli()
	}
	body, _ := json.Marshal(next)
	return &DutyBusinessEvent{ID: dutyEventID(next.TenantID, next.DeviceID, kind, next.Version), TenantID: next.TenantID, Type: kind, Source: "device", ResourceID: next.DeviceID, ResourceVersion: next.Version, DeviceID: next.DeviceID, ActorID: DutyActor(ctx), OccurredAt: at, RecordedAt: time.Now().UnixMilli(), Body: body}
}
