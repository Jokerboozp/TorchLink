package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// RetentionConfig bounds how long traffic-driven data stays in PostgreSQL
// and ClickHouse. A value of 0 days keeps the data forever. Business records
// that the operator edits (devices, rules, fire safety, users) are never
// purged by this policy.
type RetentionConfig struct {
	Enabled bool
	// At is the time of day ("HH:MM", in Timezone) of the daily run.
	At string
	// Timezone also defines the days matched against daily backups, so it
	// follows IOT_BACKUP_TIMEZONE unless set separately.
	Timezone string
	// RequireBackup restricts purging of device messages to days covered by
	// a completed DEVICE_DAILY or later FULL backup.
	RequireBackup bool
	// BatchSize rows are deleted per statement, with a short pause between
	// statements so purging never monopolizes the database.
	BatchSize  int
	BatchPause time.Duration

	AlarmDays       int
	AuditDays       int
	StandardDays    int
	RawDays         int
	StateEventDays  int
	AILogDays       int
	VideoEventDays  int
	ReservationDays int
	// ClickHouse table TTLs.
	TelemetryDays  int
	ClickRawDays   int
	retentionError error
}

func loadRetention() RetentionConfig {
	c := RetentionConfig{
		Enabled:       boolValue("IOT_RETENTION_ENABLED", true),
		At:            strings.TrimSpace(get("IOT_RETENTION_TIME", "03:30")),
		Timezone:      strings.TrimSpace(get("IOT_RETENTION_TIMEZONE", get("IOT_BACKUP_TIMEZONE", "Asia/Shanghai"))),
		RequireBackup: boolValue("IOT_RETENTION_REQUIRE_BACKUP", false),
		BatchSize:     intValue("IOT_RETENTION_BATCH_SIZE", 5000),
		BatchPause:    duration("IOT_RETENTION_BATCH_PAUSE", 200*time.Millisecond),
	}
	for _, item := range []struct {
		target   *int
		name     string
		fallback int
	}{
		{&c.AlarmDays, "IOT_RETENTION_ALARM_DAYS", 1095},
		{&c.AuditDays, "IOT_RETENTION_AUDIT_DAYS", 1095},
		{&c.StandardDays, "IOT_RETENTION_STANDARD_DAYS", 90},
		{&c.RawDays, "IOT_RETENTION_RAW_DAYS", 180},
		{&c.StateEventDays, "IOT_RETENTION_STATE_EVENT_DAYS", 90},
		{&c.AILogDays, "IOT_RETENTION_AI_LOG_DAYS", 180},
		{&c.VideoEventDays, "IOT_RETENTION_VIDEO_EVENT_DAYS", 1095},
		{&c.ReservationDays, "IOT_RETENTION_RESERVATION_DAYS", 7},
		{&c.TelemetryDays, "IOT_RETENTION_TELEMETRY_DAYS", 365},
		{&c.ClickRawDays, "IOT_RETENTION_CLICKHOUSE_RAW_DAYS", 180},
	} {
		value, err := daysValue(item.name, item.fallback)
		if err != nil && c.retentionError == nil {
			c.retentionError = err
		}
		*item.target = value
	}
	return c
}

// daysValue accepts 0 (keep forever), unlike intValue.
func daysValue(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 || v > 36500 {
		return fallback, fmt.Errorf("%s must be a number of days between 0 and 36500", name)
	}
	return v, nil
}

// Validate reports malformed retention settings.
func (c RetentionConfig) Validate() error {
	if c.retentionError != nil {
		return c.retentionError
	}
	if _, err := time.Parse("15:04", c.At); err != nil {
		return fmt.Errorf("IOT_RETENTION_TIME must be HH:MM")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("IOT_RETENTION_TIMEZONE is not a valid time zone")
	}
	// Deduplication of retransmitted raw messages relies on the reservation
	// and the raw index; the reservation must not outlive the raw index.
	if c.ReservationDays == 0 || (c.RawDays > 0 && c.ReservationDays > c.RawDays) {
		return fmt.Errorf("IOT_RETENTION_RESERVATION_DAYS must be between 1 and IOT_RETENTION_RAW_DAYS")
	}
	return nil
}

// Cutoff returns the oldest instant to keep for a retention of days, or the
// zero time when days is 0 (keep forever).
func Cutoff(now time.Time, days int) time.Time {
	if days <= 0 {
		return time.Time{}
	}
	return now.AddDate(0, 0, -days)
}
