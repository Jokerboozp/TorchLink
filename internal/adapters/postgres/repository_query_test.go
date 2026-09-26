package postgres

import (
	"strings"
	"testing"

	"iot-platform/internal/ports"
)

func TestCountManagedDeviceChildrenQueryMatchesDeviceRegistrySchema(t *testing.T) {
	query := strings.ToLower(countManagedDeviceChildrenSQL)
	if !strings.Contains(query, "body->>'gatewayid'") || strings.Contains(query, "device_registry.gateway_id") || strings.Contains(query, " and gateway_id") {
		t.Fatalf("device child count query must read gatewayId from body jsonb, not a physical gateway_id column: %s", countManagedDeviceChildrenSQL)
	}
}

func TestDeviceFilterSQLBindsEveryValue(t *testing.T) {
	where, args := deviceFilterSQL(ports.DeviceFilter{TenantID: "t", Role: "GATEWAY", RestrictProducts: true, Query: `50%_a\b`, Status: "ENABLED", Runtime: "NEVER_SEEN"})
	if len(args) != 6 || !strings.Contains(where, "$6") || strings.Contains(where, "$7") {
		t.Fatalf("placeholders do not match arguments: %s %v", where, args)
	}
	if products, ok := args[2].([]string); !ok || products == nil || len(products) != 0 {
		t.Fatalf("restricted empty product list must match nothing, got %#v", args[2])
	}
	if args[3] != `%50\%\_a\\b%` || !strings.Contains(where, `ESCAPE '\'`) {
		t.Fatalf("keyword wildcards must be escaped: %v", args[3])
	}
	for _, column := range []string{"d.body->>'deviceRole'", "d.body->>'name'", "s.business_status", "d.status", "d.product_id"} {
		if !strings.Contains(where, column) {
			t.Fatalf("missing %s in %s", column, where)
		}
	}
	if strings.Contains(where, "category") {
		t.Fatalf("roles must come from the stored device, not the template: %s", where)
	}
	if where, args = deviceFilterSQL(ports.DeviceFilter{TenantID: "t"}); where != "d.tenant_id=$1" || len(args) != 1 {
		t.Fatalf("empty filter: %s %v", where, args)
	}
}
