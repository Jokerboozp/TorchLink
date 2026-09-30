package backup

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type applicationTableSpec struct {
	Columns    map[string]knowledgeColumn
	PrimaryKey []string
}

func applicationSpec(columns, primary string) applicationTableSpec {
	s := applicationTableSpec{Columns: map[string]knowledgeColumn{}, PrimaryKey: strings.Fields(primary)}
	for _, field := range strings.Fields(columns) {
		parts := strings.SplitN(field, ":", 2)
		nullable := strings.HasSuffix(parts[1], "?")
		s.Columns[parts[0]] = knowledgeColumn{Name: parts[0], Type: strings.TrimSuffix(parts[1], "?"), NotNull: !nullable}
	}
	return s
}

// Only data descriptions from this exact whitelist are restored. Artifacts
// never provide executable SQL, triggers, functions or arbitrary table names.
var applicationSpecs = map[string]applicationTableSpec{
	"analysis_document":             applicationSpec("tenant_id:text kind:text id:text run_id:text device_id:text application_kind:text resource_id:text status:text device_ids:_text version:int8 created_at:int8 body:jsonb", "tenant_id kind id"),
	"measurement_availability":      applicationSpec("tenant_id:text message_id:text source:text available_at:int8 recorded_at:int8", "tenant_id message_id source"),
	"analytics_source_collection":   applicationSpec("source:text collection_started_at:int8 backfill_start:int8? backfill_end:int8? backfill_status:text historical_quality:text", "source"),
	"analytics_configuration_event": applicationSpec("seq:int8 tenant_id:text source:text resource_id:text resource_version:int8 device_id:text product_id:text occurred_at:int8 recorded_at:int8 body:jsonb initial_snapshot:bool", "seq"),
	"alarm_rule_revision":           applicationSpec("tenant_id:text id:text rule_id:text version:int4 hash:text registered_at:int8 body:jsonb", "tenant_id id"),
	"alarm_rule_activation":         applicationSpec("tenant_id:text rule_id:text revision_id:text version:int4 since_at:int8 deleted:bool body:jsonb", "tenant_id rule_id version"),
	"alarm_rule_current_revision":   applicationSpec("tenant_id:text rule_id:text revision_id:text version:int4 deleted:bool", "tenant_id rule_id"),
	"alarm_rule_revision_pending":   applicationSpec("tenant_id:text rule_id:text revision_id:text device_id:text since_at:int8", "tenant_id revision_id device_id"),
	"rule_evaluation_trace":         applicationSpec("tenant_id:text id:text message_id:text claim_token:int8 device_id:text message_timestamp:int8 started_at:int8 status:text body:jsonb", "tenant_id id"),
	"duty_action_link":              applicationSpec("tenant_id:text id:text version:int8 created_at:int8 updated_at:int8 body:jsonb", "tenant_id id"),
	"platform_access":               applicationSpec("tenant_id:text revision:int8 body:jsonb", "tenant_id"),
	"iot_product":                   applicationSpec("tenant_id:text id:text status:text protocol_package_id:text? body:jsonb updated_at:timestamptz", "tenant_id id"),
	"device_registry":               applicationSpec("tenant_id:text id:text product_id:text status:text access_key:text secret_hash:text body:jsonb updated_at:timestamptz", "tenant_id id"),
	"device_state":                  applicationSpec("tenant_id:text device_id:text product_id:text business_status:text last_seen_at:int8 body:jsonb updated_at:timestamptz version:int8", "tenant_id device_id"),
	"device_state_event":            applicationSpec("id:int8 tenant_id:text device_id:text business_status:text body:jsonb created_at:timestamptz", "id"),
	"alarm_rule":                    applicationSpec("tenant_id:text id:text product_id:text? enabled:bool body:jsonb updated_at:timestamptz", "tenant_id id"),
	"alarm_record":                  applicationSpec("tenant_id:text id:text rule_id:text device_id:text status:text level:text source:text last_triggered_at:int8 body:jsonb version:int8", "tenant_id id"),
	"component_alarm_state":         applicationSpec("tenant_id:text device_id:text rule_id:text body:jsonb", "tenant_id device_id rule_id"),
	"raw_archive_index":             applicationSpec("tenant_id:text product_id:text device_id:text message_id:text protocol:text? payload_format:text? object_bucket:text object_key:text object_offset:int8 payload_hash:text payload_size:int4 received_at:int8 archived_at:int8 published_at:int8 publish_attempts:int4 last_publish_error:text parse_attempted_at:int8 parse_error:text", "tenant_id message_id"),
	"raw_ingest_reservation":        applicationSpec("tenant_id:text message_id:text payload_hash:text metadata:jsonb", "tenant_id message_id"),
	"raw_message_log":               applicationSpec("tenant_id:text message_id:text product_id:text device_id:text protocol:text? payload_format:text? payload_hash:text payload_size:int4 received_at:int8 stored_at:int8 body:jsonb", "tenant_id message_id"),
	"standard_message":              applicationSpec("tenant_id:text message_id:text raw_message_id:text product_id:text device_id:text message_type:text ts:int8 properties:jsonb event:jsonb tags:jsonb body:jsonb processed_at:int8 claim_owner:text claim_token:int8 claim_expires_at:int8 attempts:int4", "tenant_id message_id"),
}

func applicationTables() []string {
	tables := make([]string, 0, len(applicationSpecs))
	for name := range applicationSpecs {
		tables = append(tables, name)
	}
	slices.Sort(tables)
	return tables
}

func validateApplicationSchema(schema knowledgeSchema) error {
	if len(schema.Tables) != len(applicationSpecs) {
		return errors.New("application schema incomplete")
	}
	seen := map[string]bool{}
	for _, table := range schema.Tables {
		expected, ok := applicationSpecs[table.Name]
		if !ok || seen[table.Name] || len(table.Columns) != len(expected.Columns) || !slices.Equal(table.PrimaryKey, expected.PrimaryKey) {
			return fmt.Errorf("invalid application table schema: %s", table.Name)
		}
		seen[table.Name] = true
		columns := map[string]bool{}
		for _, column := range table.Columns {
			if want, ok := expected.Columns[column.Name]; !ok || column != want || columns[column.Name] {
				return fmt.Errorf("invalid application column schema: %s.%s", table.Name, column.Name)
			}
			columns[column.Name] = true
		}
	}
	return nil
}
