package model

import "time"

// Tables purged by the retention job; the names are the PostgreSQL tables.
const (
	RetentionStandardMessages = "standard_message"
	RetentionRawIndex         = "raw_archive_index"
	RetentionRawLog           = "raw_message_log"
	RetentionReservations     = "raw_ingest_reservation"
	RetentionStateEvents      = "device_state_event"
	RetentionAlarms           = "alarm_record"
	RetentionAudit            = "audit_log"
	RetentionAIToolCalls      = "ai_tool_call_log"
	RetentionVideoEvents      = "video_alarm_event"
	RetentionNotifications    = "notification_task"
	// RetentionStandardKeys holds the recent standard message IDs that
	// deduplicate concurrent inserts into the partitioned standard_message.
	RetentionStandardKeys = "standard_message_key"
)

// TablePartition is one monthly partition covering [From, To).
type TablePartition struct {
	Name string
	From time.Time
	To   time.Time
}

// BackupWindow is a range of device messages covered by a completed backup;
// a zero Start means everything before End.
type BackupWindow struct {
	Start time.Time
	End   time.Time
}

// Covers reports whether the window includes the whole of [from, to).
func (w BackupWindow) Covers(from, to time.Time) bool {
	return !from.Before(w.Start) && !to.After(w.End)
}

// DeadLetter is one message moved to a dead-letter topic iot.dlq.<group>.
type DeadLetter struct {
	Group           string    `json:"group"`
	Partition       int       `json:"partition"`
	Offset          int64     `json:"offset"`
	Time            time.Time `json:"time"`
	Key             string    `json:"key"`
	SourceTopic     string    `json:"sourceTopic"`
	Error           string    `json:"error"`
	RetryCount      int       `json:"retryCount"`
	Payload         string    `json:"payload"`
	PayloadEncoding string    `json:"payloadEncoding,omitempty"`
	Truncated       bool      `json:"truncated,omitempty"`
}

// DeadLetterGroups are the consumer groups whose dead letters can be viewed
// and replayed, with the only topic each may be replayed to.
var DeadLetterGroups = map[string]string{
	"parser":                       TopicRaw,
	"processor":                    TopicDeviceBusiness,
	"state":                        TopicDeviceState,
	"device-alarm-notifications":   TopicAlarmReported,
	"alarm-notifications":          TopicAlarmReported,
	"alarm-recovery-notifications": TopicAlarmRecovered,
}
