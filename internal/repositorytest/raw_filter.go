package repositorytest

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"reflect"
	"testing"
	"time"
)

func RawFilters(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	for _, row := range []model.RawArchiveIndex{
		{TenantID: "raw-filter", MessageID: "r1", DeviceID: "d1", ProductID: "p1", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1000},
		{TenantID: "raw-filter", MessageID: "r2", DeviceID: "d1", ProductID: "p1", Protocol: "tcp", PayloadFormat: "hex", ReceivedAt: 2000, ParseError: "invalid frame"},
		{TenantID: "raw-filter", MessageID: "r3", DeviceID: "d2", ProductID: "p2", Protocol: "json", PayloadFormat: "json", ReceivedAt: 3000},
		{TenantID: "raw-filter", MessageID: "r4", DeviceID: "d2", ProductID: "p2", Protocol: "json", PayloadFormat: "json", ReceivedAt: 3000, ParseError: "old error"},
		{TenantID: "foreign", MessageID: "r3", DeviceID: "d2", ProductID: "p2", ReceivedAt: 4000},
	} {
		if _, err := repo.SaveRawIndex(ctx, row); err != nil {
			t.Fatal(err)
		}
		if row.ParseError != "" {
			if err := repo.MarkRawParseResult(ctx, row.TenantID, row.MessageID, row.ReceivedAt, row.ReceivedAt, row.ParseError); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, row := range []model.StandardMessage{
		{TenantID: "raw-filter", MessageID: "s1", RawMessageID: "r1", DeviceID: "d1", MessageType: model.AlarmReport, Parser: "json_parser", Timestamp: 1000},
		{TenantID: "raw-filter", MessageID: "s4-old", RawMessageID: "r4", DeviceID: "d2", MessageType: model.EventReport, Parser: "old_parser", Timestamp: 2000},
		{TenantID: "raw-filter", MessageID: "s4", RawMessageID: "r4", DeviceID: "d2", MessageType: model.PropertyReport, Parser: "json_parser", Timestamp: 3000},
		{TenantID: "foreign", MessageID: "foreign-s3", RawMessageID: "r3", DeviceID: "d2", MessageType: model.AlarmReport, Timestamp: 4000},
	} {
		if err := repo.SaveStandardMessage(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		f    ports.RawFilter
		want []string
	}{
		{"all", ports.RawFilter{}, []string{"r4", "r3", "r2", "r1"}},
		{"combined", ports.RawFilter{DeviceID: "d1", ProductID: "p1", Protocol: "JSON", PayloadFormat: "JSON", MessageType: "ALARM_REPORT", ParseStatus: "PARSED", Parser: "json_parser", Start: 1000, End: 1000}, []string{"r1"}},
		{"failed", ports.RawFilter{ParseStatus: "FAILED"}, []string{"r2"}},
		{"unparsed-cross-tenant", ports.RawFilter{ParseStatus: "UNPARSED"}, []string{"r3"}},
		{"parsed-old-error", ports.RawFilter{ParseStatus: "PARSED"}, []string{"r4", "r1"}},
		{"latest-only", ports.RawFilter{MessageType: "EVENT_REPORT"}, []string{}},
		{"message", ports.RawFilter{MessageID: "r2"}, []string{"r2"}},
		{"device-set", ports.RawFilter{DeviceIDs: []string{"d2", "missing"}}, []string{"r4", "r3"}},
		{"empty-device-set", ports.RawFilter{DeviceIDs: []string{}}, []string{}},
		{"product", ports.RawFilter{ProductID: "p2"}, []string{"r4", "r3"}},
		{"protocol", ports.RawFilter{Protocol: "TCP"}, []string{"r2"}},
		{"format", ports.RawFilter{PayloadFormat: "hex"}, []string{"r2"}},
		{"old-parser", ports.RawFilter{Parser: "old_parser"}, []string{}},
		{"injection-is-literal", ports.RawFilter{MessageID: "r1' OR 1=1 --"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.f.TenantID = "raw-filter"
			count, err := repo.CountRawIndexes(ctx, tc.f)
			if err != nil || count != len(tc.want) {
				t.Fatalf("count %d %v want %d", count, err, len(tc.want))
			}
			tc.f.Limit = 1
			got := []string{}
			for offset := 0; offset <= len(tc.want); offset++ {
				tc.f.Offset = offset
				rows, err := repo.ListRawIndexes(ctx, tc.f)
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					got = append(got, row.MessageID)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("paged IDs %v want %v", got, tc.want)
			}
			// A count limit caps the total without changing smaller counts.
			tc.f.CountLimit = 2
			if count, err = repo.CountRawIndexes(ctx, tc.f); err != nil || count != min(len(tc.want), 2) {
				t.Fatalf("capped count %d %v want %d", count, err, min(len(tc.want), 2))
			}
		})
	}
}

type rawLookupRepository interface {
	SaveRawIndex(context.Context, model.RawArchiveIndex) (bool, error)
	GetRawIndexAt(context.Context, string, string, int64) (model.RawArchiveIndex, error)
}

// RawIndexLookupByReceiveTime checks that a receive-time hint finds the
// message, and that a wrong or missing hint still finds it.
func RawIndexLookupByReceiveTime(t *testing.T, repo rawLookupRepository) {
	t.Helper()
	ctx := context.Background()
	received := time.Date(2026, 3, 31, 23, 30, 0, 0, time.UTC).UnixMilli()
	if _, err := repo.SaveRawIndex(ctx, model.RawArchiveIndex{MessageID: "raw-at", TenantID: "raw-lookup", ProductID: "p", DeviceID: "d", Protocol: "json", PayloadFormat: "json", ObjectKey: "raw-at", PayloadHash: "h", PayloadSize: 1, ReceivedAt: received, ArchivedAt: received}); err != nil {
		t.Fatal(err)
	}
	for name, hint := range map[string]int64{"exact": received, "other month": received - 60*24*time.Hour.Milliseconds(), "unknown": 0} {
		v, err := repo.GetRawIndexAt(ctx, "raw-lookup", "raw-at", hint)
		if err != nil || v.MessageID != "raw-at" || v.ReceivedAt != received {
			t.Errorf("%s hint: %+v, %v", name, v, err)
		}
	}
	if _, err := repo.GetRawIndexAt(ctx, "raw-lookup", "missing", received); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("a missing message = %v, want ErrNotFound", err)
	}
}

type rawPagingRepository interface {
	SaveRawIndex(context.Context, model.RawArchiveIndex) (bool, error)
	ListRawIndexes(context.Context, ports.RawFilter) ([]model.RawArchiveIndex, error)
}

// RawCursorPaging checks that continuing after the last row of a page gives
// the same rows as the offset of the next page, including rows that share a
// receive time.
func RawCursorPaging(t *testing.T, repo rawPagingRepository) {
	t.Helper()
	ctx := context.Background()
	for i, at := range []int64{5000, 4000, 4000, 4000, 3000, 2000} {
		id := string(rune('a' + i))
		if _, err := repo.SaveRawIndex(ctx, model.RawArchiveIndex{MessageID: "cursor-" + id, TenantID: "raw-cursor", ProductID: "p", DeviceID: "d", Protocol: "json", PayloadFormat: "json", ObjectKey: id, PayloadHash: "h", PayloadSize: 1, ReceivedAt: at, ArchivedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: "raw-cursor", Limit: 10})
	if err != nil || len(all) != 6 {
		t.Fatalf("listing = %v, %v", all, err)
	}
	var cursor *ports.RawCursor
	var walked []string
	for range 3 {
		page, err := repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: "raw-cursor", Limit: 2, Offset: 99, After: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if cursor == nil {
			// The first page has no cursor and uses the offset.
			page, _ = repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: "raw-cursor", Limit: 2})
		}
		for _, v := range page {
			walked = append(walked, v.MessageID)
		}
		last := page[len(page)-1]
		cursor = &ports.RawCursor{ReceivedAt: last.ReceivedAt, MessageID: last.MessageID}
	}
	for i, v := range all {
		if walked[i] != v.MessageID {
			t.Fatalf("cursor pages %v, want the offset order %v", walked, all)
		}
	}
	if rest, _ := repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: "raw-cursor", Limit: 2, After: cursor}); len(rest) != 0 {
		t.Fatalf("after the last row: %v", rest)
	}
}
