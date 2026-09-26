package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNextDailyRun(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 8, 31, 23, 58, 0, 0, location)
	next, err := nextDailyRun(now, "23:59", location)
	if err != nil {
		t.Fatalf("next daily run: %v", err)
	}
	want := time.Date(2026, 8, 31, 23, 59, 0, 0, location)
	if !next.Equal(want) {
		t.Fatalf("next daily run = %s, want %s", next, want)
	}

	next, err = nextDailyRun(want, "23:59", location)
	if err != nil {
		t.Fatalf("next day run: %v", err)
	}
	want = time.Date(2026, 9, 1, 23, 59, 0, 0, location)
	if !next.Equal(want) {
		t.Fatalf("next day run = %s, want %s", next, want)
	}
}

func TestNextDailyRunRejectsInvalidClock(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	if _, err := nextDailyRun(time.Now().In(location), "midnight", location); err == nil {
		t.Fatal("expected invalid clock error")
	}
}

func TestScheduledRawLogBackupTargetsPreviousDay(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	next := time.Date(2026, 9, 1, 0, 5, 0, 0, location)
	backupDay := next.AddDate(0, 0, -1)
	want := time.Date(2026, 8, 31, 0, 5, 0, 0, location)
	if !backupDay.Equal(want) {
		t.Fatalf("backup day = %s, want %s", backupDay, want)
	}
}

func TestRespondUsesServerErrorForBackupExecutionFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	respond(recorder, nil, errTestBackupFailure{})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload["error"], "backup failed") {
		t.Fatalf("unexpected response body: %s", recorder.Body.String())
	}
}

type errTestBackupFailure struct{}

func (errTestBackupFailure) Error() string { return "backup failed" }
