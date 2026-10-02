package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPruneCapacityAccessPreservesNumbersAndEffectiveScopes(t *testing.T) {
	body := []byte(`{"unknown":9007199254740993,"users":[{"username":"all","deviceScope":"all","deviceIds":["fixture","real"],"sessionVersion":9007199254740993,"extra":"keep"},{"username":"selected","deviceScope":"selected","deviceIds":["fixture","real"],"sessionVersion":9007199254740993},{"username":"inherits-all","deviceScope":"inherit","roleIds":["selected","all"],"sessionVersion":40},{"username":"inherits-selected","deviceScope":"inherit","roleIds":["selected"],"sessionVersion":50}],"roles":[{"id":"selected","deviceScope":"selected","deviceIds":["fixture","real"]},{"id":"all","deviceScope":"all","deviceIds":["fixture"]}],"apiKeys":[{"secretHash":"preserve"}]}`)
	next, count, err := PruneCapacityAccessReferences(body, []string{"fixture"})
	if err != nil || count != 4 {
		t.Fatal(count, err)
	}
	if !strings.Contains(string(next), `"unknown":9007199254740993`) || !strings.Contains(string(next), `"extra":"keep"`) {
		t.Fatal("unknown fields/numeric precision lost", string(next))
	}
	var state AccessState
	if err = json.Unmarshal(next, &state); err != nil {
		t.Fatal(err)
	}
	want := []int64{9007199254740993, 9007199254740994, 40, 51}
	for i, user := range state.Users {
		if user.SessionVersion != want[i] {
			t.Fatal("effective scope session", user)
		}
	}
	if len(state.Users[0].DeviceIDs) != 1 || state.Users[0].DeviceIDs[0] != "real" || len(state.Roles[1].DeviceIDs) != 0 || len(state.APIKeys) != 1 || state.APIKeys[0].SecretHash != "preserve" {
		t.Fatal("scoped references or unrelated auth state", state)
	}
	unchanged, count, err := PruneCapacityAccessReferences(next, []string{"fixture"})
	if err != nil || count != 0 || string(unchanged) != string(next) {
		t.Fatal("idempotent prune changed state", count, err)
	}
}

func TestCapacityCleanupCountsAddRuntimeWarnings(t *testing.T) {
	c := CapacityCleanupCounts{Warnings: []string{"mixed report retained"}}
	c.Add(CapacityCleanupCounts{Inbox: 2, InboxSkipped: 1, RetainedRequests: 3, QueueOffsetSpan: 4, QueueSkippedPartitions: 5, Warnings: []string{"mixed report retained", "active queue retained"}})
	if c.Inbox != 2 || c.InboxSkipped != 1 || c.RetainedRequests != 3 || c.QueueOffsetSpan != 4 || c.QueueSkippedPartitions != 5 || len(c.Warnings) != 2 {
		t.Fatal(c)
	}
}

func TestPruneCapacityAccessRejectsUnrepresentableSessionVersion(t *testing.T) {
	for _, version := range []string{`9223372036854775807`, `"123"`} {
		if _, _, err := PruneCapacityAccessReferences([]byte(`{"users":[{"deviceScope":"selected","deviceIds":["fixture"],"sessionVersion":`+version+`}]}`), []string{"fixture"}); err == nil {
			t.Fatal("invalid session version accepted", version)
		}
	}
}
