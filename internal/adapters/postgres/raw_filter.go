package postgres

import (
	"fmt"
	"iot-platform/internal/ports"
	"strings"
)

// List and count share predicates, so filters are applied before pagination.
func rawFilterSQL(f ports.RawFilter) (string, []any) {
	from := "raw_archive_index r"
	var args []any
	conditions := []string{"TRUE"}
	add := func(column string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(column, len(args)))
	}
	for _, field := range []struct{ column, value string }{
		{"r.tenant_id", f.TenantID}, {"r.product_id", f.ProductID}, {"r.device_id", f.DeviceID}, {"r.message_id", f.MessageID},
	} {
		if field.value != "" {
			add(field.column+"=$%d", field.value)
		}
	}
	if f.DeviceIDs != nil {
		add("r.device_id=ANY($%d)", f.DeviceIDs)
	}
	if f.Protocol != "" {
		add("lower(r.protocol)=lower($%d)", f.Protocol)
	}
	if f.PayloadFormat != "" {
		add("lower(r.payload_format)=lower($%d)", f.PayloadFormat)
	}
	if f.Start > 0 {
		add("r.received_at >= $%d", f.Start)
	}
	if f.End > 0 {
		add("r.received_at <= $%d", f.End)
	}
	if f.ParseStatus != "" || f.MessageType != "" || f.Parser != "" {
		// Same latest-message ordering as GetStandardMessagesByRawIDs. A lateral
		// single row avoids duplicate raw rows when parsing produced several results.
		from += ` LEFT JOIN LATERAL (SELECT message_id,message_type,body->>'parser' AS parser FROM standard_message WHERE tenant_id=r.tenant_id AND raw_message_id=r.message_id ORDER BY ts DESC,message_id DESC LIMIT 1) s ON TRUE`
		switch f.ParseStatus {
		case "PARSED":
			conditions = append(conditions, "s.message_id IS NOT NULL")
		case "FAILED":
			conditions = append(conditions, "s.message_id IS NULL AND r.parse_error<>''")
		case "UNPARSED":
			conditions = append(conditions, "s.message_id IS NULL AND r.parse_error=''")
		}
		if f.MessageType != "" {
			add("s.message_type=$%d", f.MessageType)
		}
		if f.Parser != "" {
			add("s.parser=$%d", f.Parser)
		}
	}
	return from + " WHERE " + strings.Join(conditions, " AND "), args
}
