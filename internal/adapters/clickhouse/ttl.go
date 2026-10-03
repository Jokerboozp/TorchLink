package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ttlExpressions is the time each storage table expires by.
var ttlExpressions = map[string]string{
	"iot_telemetry":   "toDateTime(ts)",
	"iot_raw_message": "toDateTime(fromUnixTimestamp64Milli(received_at))",
}

const ttlCommentPrefix = "iot_ttl_days="

// applyTTL sets each table's TTL to the configured days (0 removes it). The
// applied value is kept in the table comment so a restart issues no ALTER;
// existing parts are not rewritten (materialize_ttl_after_modify=0) and
// ttl_only_drop_parts drops whole expired monthly partitions in the
// background instead of rewriting parts row by row.
func (r *Repository) applyTTL(ctx context.Context) error {
	want := map[string]int{"iot_telemetry": r.opts.TelemetryTTLDays, "iot_raw_message": r.opts.RawTTLDays}
	current, err := r.ttlComments(ctx)
	if err != nil {
		return err
	}
	for table, days := range want {
		if days < 0 {
			return fmt.Errorf("clickhouse TTL days must not be negative")
		}
		target := table
		onCluster := ""
		if r.opts.Cluster != "" {
			target = table + "_local"
			onCluster = " ON CLUSTER " + r.opts.Cluster
		}
		applied, known := current[target]
		if known && applied == days || !known && days == 0 {
			continue
		}
		var statements []string
		if days > 0 {
			statements = append(statements,
				fmt.Sprintf("ALTER TABLE %s%s MODIFY SETTING ttl_only_drop_parts=1", target, onCluster),
				fmt.Sprintf("ALTER TABLE %s%s MODIFY TTL %s + INTERVAL %d DAY SETTINGS materialize_ttl_after_modify=0", target, onCluster, ttlExpressions[table], days))
		} else {
			statements = append(statements, fmt.Sprintf("ALTER TABLE %s%s REMOVE TTL", target, onCluster))
		}
		statements = append(statements, fmt.Sprintf("ALTER TABLE %s%s MODIFY COMMENT '%s%d'", target, onCluster, ttlCommentPrefix, days))
		for _, statement := range statements {
			if _, err := r.query(ctx, statement, nil); err != nil {
				return fmt.Errorf("set TTL of %s: %w", target, err)
			}
		}
	}
	return nil
}

// ttlComments reads the TTL days recorded in the table comments.
func (r *Repository) ttlComments(ctx context.Context) (map[string]int, error) {
	body, err := r.query(ctx, "SELECT name, comment FROM system.tables WHERE database=currentDatabase() FORMAT JSONEachRow", nil)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		var row struct{ Name, Comment string }
		if len(line) == 0 || json.Unmarshal(line, &row) != nil || !strings.HasPrefix(row.Comment, ttlCommentPrefix) {
			continue
		}
		if days, err := strconv.Atoi(strings.TrimPrefix(row.Comment, ttlCommentPrefix)); err == nil {
			out[row.Name] = days
		}
	}
	return out, nil
}
