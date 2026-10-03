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
)

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
