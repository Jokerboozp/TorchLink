package postgres

import (
	"strings"
	"testing"

	"iot-platform/internal/ports"
)

// Only the filters that are set reach the SQL, so PostgreSQL cannot fall back
// to a generic plan that scans every alarm.
func TestAlarmFilterSQLUsesOnlySetFilters(t *testing.T) {
	where, args := alarmFilterSQL(ports.AlarmFilter{TenantID: "tenant", Status: "ACTIVE", DeviceIDs: []string{"a", "b"}})
	if where != " WHERE tenant_id=$1 AND status=$2 AND device_id=ANY($3)" || len(args) != 3 {
		t.Fatalf("where=%q args=%v", where, args)
	}
	if strings.Contains(where, "''") {
		t.Fatal("catch-all conditions must not be generated")
	}
	if where, args = alarmFilterSQL(ports.AlarmFilter{}); where != "" || len(args) != 0 {
		t.Fatalf("an empty filter must not add conditions: %q %v", where, args)
	}
	if where, _ = alarmFilterSQL(ports.AlarmFilter{TenantID: "tenant", DeviceIDs: []string{}}); !strings.Contains(where, "device_id=ANY($2)") {
		t.Fatalf("an empty grant must still restrict: %q", where)
	}
}
