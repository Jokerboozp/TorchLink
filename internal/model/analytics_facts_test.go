package model

import "testing"

func TestFactQueryRequiresExplicitDeviceScopeAndHalfOpenInterval(t *testing.T) {
	for _, q := range []FactQuery{{Start: 1, End: 2}, {DeviceIDs: []string{""}, Start: 1, End: 2}, {DeviceIDs: []string{"d"}, Start: 2, End: 2}, {DeviceIDs: []string{"d"}, Start: 1, End: 2, Limit: 1001}, {DeviceIDs: []string{"d"}, Start: 1, End: 2, TimeBasis: "TIMESTAMP"}, {DeviceIDs: []string{"d"}, Start: 1, End: 2, AvailabilitySource: "receive_time"}} {
		if q.Validate() == nil {
			t.Fatalf("invalid query accepted: %+v", q)
		}
	}
	if err := (FactQuery{DeviceIDs: []string{"d"}, Start: 0, End: 100, TimeBasis: "AVAILABLE"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
