// Package retention deletes traffic-driven data older than the configured
// retention so PostgreSQL stays bounded. ClickHouse tables use table TTLs
// set by the ClickHouse adapter instead.
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"iot-platform/internal/config"
	"iot-platform/internal/model"
)

// Store is implemented by the PostgreSQL repository.
type Store interface {
	PurgeRange(ctx context.Context, table string, from, to time.Time, limit int) (int64, error)
	OldestRetained(ctx context.Context, table string) (time.Time, bool, error)
	BackupWindows(ctx context.Context) ([]model.BackupWindow, error)
}

// PartitionStore is implemented by a store whose large tables are
// partitioned by month; whole expired months are dropped instead of deleted
// row by row.
type PartitionStore interface {
	EnsurePartitions(ctx context.Context, now time.Time) error
	ExpiredPartitions(ctx context.Context, table string, cutoff time.Time) ([]model.TablePartition, error)
	DropPartition(ctx context.Context, table, partition string) (bool, error)
	DropEmptyLegacy(ctx context.Context, table string) (bool, error)
}

// Metrics receives counters; *metrics.Registry implements it.
type Metrics interface {
	Add(string, uint64)
	Inc(string)
	Set(string, float64)
}

// deviceMessageTables are exported by the daily backup; RequireBackup gates
// their purge on backup coverage.
var deviceMessageTables = map[string]bool{
	model.RetentionStandardMessages: true,
	model.RetentionRawIndex:         true,
	model.RetentionRawLog:           true,
}

type Service struct {
	cfg      config.RetentionConfig
	store    Store
	metrics  Metrics
	log      *slog.Logger
	location *time.Location
	now      func() time.Time
	sleep    func(context.Context, time.Duration)
	lastRun  string
	// lastMaintained is the day partitions were last created ahead.
	lastMaintained string
}

func New(cfg config.RetentionConfig, store Store, metrics Metrics, log *slog.Logger, location *time.Location) *Service {
	if location == nil {
		location = time.Local
	}
	if log == nil {
		log = slog.Default()
	}
	return &Service{cfg: cfg, store: store, metrics: metrics, log: log, location: location, now: time.Now, sleep: sleepCtx}
}

// Table is one retention rule.
type Table struct {
	Name string
	Days int
}

func (s *Service) tables() []Table {
	c := s.cfg
	return []Table{
		{model.RetentionReservations, c.ReservationDays},
		{model.RetentionStandardKeys, c.ReservationDays},
		{model.RetentionStandardMessages, c.StandardDays},
		{model.RetentionRawIndex, c.RawDays},
		{model.RetentionRawLog, c.RawDays},
		{model.RetentionStateEvents, c.StateEventDays},
		{model.RetentionAIToolCalls, c.AILogDays},
		{model.RetentionAIRuns, c.AILogDays},
		{model.RetentionVideoEvents, c.VideoEventDays},
		{model.RetentionAlarms, c.AlarmDays},
		{model.RetentionNotifications, c.AlarmDays},
		{model.RetentionAudit, c.AuditDays},
	}
}

// Due reports whether the daily run has not happened yet today and the
// configured time has passed. The scheduler checks it periodically.
func (s *Service) Due() bool {
	now := s.now().In(s.location)
	at, err := time.Parse("15:04", s.cfg.At)
	if err != nil {
		return false
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), at.Hour(), at.Minute(), 0, 0, s.location)
	return now.After(start) && s.lastRun != now.Format(time.DateOnly)
}

// Tick runs the purge when it is due. It is safe to call from the cluster
// singleton scheduler; a takeover on another instance may repeat a day's
// purge, which only finds nothing left to delete.
func (s *Service) Tick(ctx context.Context) error {
	// Partitions are created ahead every day even when retention is off.
	if ps, ok := s.store.(PartitionStore); ok {
		if today := s.now().In(s.location).Format(time.DateOnly); s.lastMaintained != today {
			if err := ps.EnsurePartitions(ctx, s.now()); err != nil {
				s.inc("partition_maintenance_failed_total")
				s.log.Error("create upcoming partitions", "error", err)
			} else {
				s.lastMaintained = today
			}
		}
	}
	if !s.cfg.Enabled || !s.Due() {
		return nil
	}
	_, err := s.RunOnce(ctx)
	if err == nil {
		s.lastRun = s.now().In(s.location).Format(time.DateOnly)
	}
	return err
}

// Result lists the rows deleted per table in one run.
type Result map[string]int64

// RunOnce purges every table once. A failing table is reported and the
// remaining tables still run.
func (s *Service) RunOnce(ctx context.Context) (Result, error) {
	result := Result{}
	var failures []error
	var windows []model.BackupWindow
	if s.cfg.RequireBackup {
		var err error
		if windows, err = s.store.BackupWindows(ctx); err != nil {
			s.inc("retention_failed_total")
			return result, fmt.Errorf("read backup coverage: %w", err)
		}
	}
	for _, table := range s.tables() {
		if table.Days <= 0 {
			continue
		}
		cutoff := config.Cutoff(s.now(), table.Days)
		var deleted int64
		var err error
		if err = s.dropPartitions(ctx, table.Name, cutoff, windows); err != nil {
			s.inc("retention_failed_total")
			s.log.Error("retention partition drop failed", "table", table.Name, "error", err)
		}
		if s.cfg.RequireBackup && deviceMessageTables[table.Name] {
			deleted, err = s.purgeCovered(ctx, table.Name, cutoff, windows)
		} else {
			deleted, err = s.purge(ctx, table.Name, time.Time{}, cutoff)
		}
		result[table.Name] = deleted
		if deleted > 0 && s.metrics != nil {
			s.metrics.Add("retention_deleted_total", uint64(deleted))
			s.metrics.Add("retention_deleted_"+table.Name+"_total", uint64(deleted))
		}
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			s.inc("retention_failed_total")
			s.log.Error("retention purge failed", "table", table.Name, "error", err)
			failures = append(failures, fmt.Errorf("%s: %w", table.Name, err))
			continue
		}
		if deleted > 0 {
			s.log.Info("retention purge", "table", table.Name, "deleted", deleted, "before", cutoff.Format(time.RFC3339))
		}
		if ps, ok := s.store.(PartitionStore); ok {
			if dropped, err := ps.DropEmptyLegacy(ctx, table.Name); err != nil {
				s.log.Warn("drop emptied legacy partition", "table", table.Name, "error", err)
			} else if dropped {
				s.log.Info("retention dropped emptied legacy partition", "table", table.Name)
			}
		}
	}
	if len(failures) == 0 && s.metrics != nil {
		s.metrics.Set("retention_last_success_timestamp_seconds", float64(s.now().Unix()))
	}
	return result, errors.Join(failures...)
}

func (s *Service) purge(ctx context.Context, table string, from, to time.Time) (int64, error) {
	var total int64
	for {
		n, err := s.store.PurgeRange(ctx, table, from, to, s.cfg.BatchSize)
		total += n
		if err != nil || n < int64(max(s.cfg.BatchSize, 1)) {
			return total, err
		}
		s.sleep(ctx, s.cfg.BatchPause)
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

// purgeCovered deletes, day by day in the backup time zone, only the days
// that a completed backup covers; uncovered days are kept and reported.
func (s *Service) purgeCovered(ctx context.Context, table string, cutoff time.Time, windows []model.BackupWindow) (int64, error) {
	oldest, found, err := s.store.OldestRetained(ctx, table)
	if err != nil || !found || !oldest.Before(cutoff) {
		return 0, err
	}
	var total int64
	skipped := 0
	day := oldest.In(s.location)
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, s.location)
	for day.Before(cutoff) {
		next := day.AddDate(0, 0, 1)
		end := next
		if end.After(cutoff) {
			end = cutoff
		}
		if covered(windows, day, next) {
			n, err := s.purge(ctx, table, day, end)
			total += n
			if err != nil {
				return total, err
			}
		} else {
			skipped++
		}
		day = next
	}
	if skipped > 0 {
		s.log.Warn("retention kept days without a completed backup", "table", table, "days", skipped)
		if s.metrics != nil {
			s.metrics.Set("retention_unbacked_days_"+table, float64(skipped))
		}
	}
	return total, nil
}

// dropPartitions drops the monthly partitions that ended before cutoff
// (and, with RequireBackup, whose whole month a backup covers), then the
// legacy partition once purging emptied it. Rows of partitions that still
// hold pending messages are left to the row purge.
func (s *Service) dropPartitions(ctx context.Context, table string, cutoff time.Time, windows []model.BackupWindow) error {
	ps, ok := s.store.(PartitionStore)
	if !ok {
		return nil
	}
	parts, err := ps.ExpiredPartitions(ctx, table, cutoff)
	if err != nil {
		return err
	}
	for _, p := range parts {
		if s.cfg.RequireBackup && deviceMessageTables[table] && !s.coveredRange(windows, p.From, p.To) {
			continue
		}
		dropped, err := ps.DropPartition(ctx, table, p.Name)
		if err != nil {
			return err
		}
		if dropped {
			s.inc("retention_partitions_dropped_total")
			s.log.Info("retention dropped partition", "table", table, "partition", p.Name)
		}
	}
	return nil
}

// coveredRange reports whether completed backups cover all of [from, to),
// checking each backup-day slice separately.
func (s *Service) coveredRange(windows []model.BackupWindow, from, to time.Time) bool {
	for at := from; at.Before(to); {
		local := at.In(s.location)
		next := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.location).AddDate(0, 0, 1)
		if next.After(to) {
			next = to
		}
		if !covered(windows, at, next) {
			return false
		}
		at = next
	}
	return true
}

func covered(windows []model.BackupWindow, from, to time.Time) bool {
	for _, w := range windows {
		if w.Covers(from, to) {
			return true
		}
	}
	return false
}

func (s *Service) inc(name string) {
	if s.metrics != nil {
		s.metrics.Inc(name)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
