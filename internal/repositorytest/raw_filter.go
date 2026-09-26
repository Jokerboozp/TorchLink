package repositorytest

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"reflect"
	"testing"
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
			if err := repo.MarkRawParseResult(ctx, row.TenantID, row.MessageID, row.ReceivedAt, row.ParseError); err != nil {
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
		})
	}
}
