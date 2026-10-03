package retention

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"iot-platform/internal/config"
	"iot-platform/internal/model"
)

type call struct {
	table    string
	from, to time.Time
}

type fakeStore struct {
	rows    map[string]int64
	calls   []call
	oldest  time.Time
	windows []model.BackupWindow
	fail    string
}

func (f *fakeStore) PurgeRange(_ context.Context, table string, from, to time.Time, limit int) (int64, error) {
	f.calls = append(f.calls, call{table, from, to})
	if table == f.fail {
		return 0, errors.New("boom")
	}
	n := min(f.rows[table], int64(limit))
	f.rows[table] -= n
	return n, nil
}
func (f *fakeStore) OldestRetained(context.Context, string) (time.Time, bool, error) {
	return f.oldest, !f.oldest.IsZero(), nil
}
func (f *fakeStore) BackupWindows(context.Context) ([]model.BackupWindow, error) {
	return f.windows, nil
}

type counters map[string]float64

func (c counters) Add(n string, v uint64)  { c[n] += float64(v) }
func (c counters) Inc(n string)            { c[n]++ }
func (c counters) Set(n string, v float64) { c[n] = v }

func testConfig() config.RetentionConfig {
	return config.RetentionConfig{Enabled: true, At: "03:30", BatchSize: 2, AlarmDays: 1095, AuditDays: 1095, StandardDays: 90, RawDays: 180, StateEventDays: 90, ReservationDays: 7}
}

func TestRunOncePurgesInBatchesAndKeepsDisabledTables(t *testing.T) {
	now := time.Date(2026, 10, 3, 4, 0, 0, 0, time.UTC)
	store := &fakeStore{rows: map[string]int64{model.RetentionStandardMessages: 5, model.RetentionAIToolCalls: 9}}
	metrics := counters{}
	s := New(testConfig(), store, metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), time.UTC)
	s.now, s.sleep = func() time.Time { return now }, func(context.Context, time.Duration) {}
	result, err := s.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result[model.RetentionStandardMessages] != 5 || metrics["retention_deleted_total"] != 5 {
		t.Fatalf("result=%v metrics=%v", result, metrics)
	}
	standard := 0
	for _, c := range store.calls {
		if c.table == model.RetentionAIToolCalls {
			t.Fatal("a table with 0 days must be kept forever")
		}
		if c.table == model.RetentionStandardMessages {
			standard++
			if !c.to.Equal(now.AddDate(0, 0, -90)) || !c.from.IsZero() {
				t.Fatalf("cutoff %v", c)
			}
		}
	}
	if standard != 3 { // 2 + 2 + 1, the short batch ends the loop
		t.Fatalf("batches=%d", standard)
	}
	if metrics["retention_last_success_timestamp_seconds"] != float64(now.Unix()) {
		t.Fatal("success time not recorded")
	}
}

func TestRunOnceContinuesAfterTableFailure(t *testing.T) {
	store := &fakeStore{rows: map[string]int64{model.RetentionAudit: 1}, fail: model.RetentionStandardMessages}
	metrics := counters{}
	s := New(testConfig(), store, metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), time.UTC)
	s.sleep = func(context.Context, time.Duration) {}
	result, err := s.RunOnce(context.Background())
	if err == nil || metrics["retention_failed_total"] != 1 || result[model.RetentionAudit] != 1 {
		t.Fatalf("err=%v metrics=%v result=%v", err, metrics, result)
	}
	if _, ok := metrics["retention_last_success_timestamp_seconds"]; ok {
		t.Fatal("a failed run must not report success")
	}
}

func TestRequireBackupPurgesOnlyCoveredDays(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 3, 4, 0, 0, 0, loc)
	cfg := testConfig()
	cfg.RequireBackup, cfg.StandardDays = true, 2
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, loc) }
	store := &fakeStore{rows: map[string]int64{}, oldest: day(28).Add(5 * time.Hour), windows: []model.BackupWindow{
		{Start: day(28), End: day(29)},
		{Start: day(30), End: day(30).AddDate(0, 0, 1)},
	}}
	s := New(cfg, store, counters{}, slog.New(slog.NewTextHandler(io.Discard, nil)), loc)
	s.now, s.sleep = func() time.Time { return now }, func(context.Context, time.Duration) {}
	if _, err := s.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	purged := []time.Time{}
	for _, c := range store.calls {
		if c.table == model.RetentionStandardMessages {
			purged = append(purged, c.from)
		}
		if c.table == model.RetentionRawIndex || c.table == model.RetentionRawLog {
			if c.from.IsZero() {
				t.Fatal("device message tables must be purged per covered day")
			}
		}
	}
	// 09-28 and 09-30 are covered; 09-29 has no backup; 10-01 is the cutoff day.
	if len(purged) != 2 || !purged[0].Equal(day(28)) || !purged[1].Equal(day(30)) {
		t.Fatalf("purged days %v", purged)
	}
	for _, c := range store.calls {
		if c.table == model.RetentionAudit && !c.from.IsZero() {
			t.Fatal("non-message tables are not gated on backups")
		}
	}
}

func TestTickRunsOncePerDayAfterConfiguredTime(t *testing.T) {
	store := &fakeStore{rows: map[string]int64{}}
	s := New(testConfig(), store, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.UTC)
	now := time.Date(2026, 10, 3, 3, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	_ = s.Tick(context.Background())
	if len(store.calls) != 0 {
		t.Fatal("ran before the configured time")
	}
	now = now.Add(time.Hour)
	_ = s.Tick(context.Background())
	runs := len(store.calls)
	if runs == 0 {
		t.Fatal("did not run after the configured time")
	}
	_ = s.Tick(context.Background())
	if len(store.calls) != runs {
		t.Fatal("ran twice on the same day")
	}
	now = now.AddDate(0, 0, 1)
	_ = s.Tick(context.Background())
	if len(store.calls) == runs {
		t.Fatal("did not run the next day")
	}
}
