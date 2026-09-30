package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"strings"
)

func (r *Repository) CapacityMessageIDs(ctx context.Context, t string, q model.CapacityCleanupBatch) ([]string, error) {
	c, ok := r.Repository.(ports.CapacityDataCleaner)
	if !ok {
		return nil, errors.New("capacity cleanup is unsupported")
	}
	return c.CapacityMessageIDs(ctx, t, q)
}

func (r *Repository) CleanupCapacityData(ctx context.Context, t string, q model.CapacityCleanupBatch) (model.CapacityCleanupCounts, error) {
	var n model.CapacityCleanupCounts
	c, ok := r.Repository.(ports.CapacityDataCleaner)
	if !ok {
		return n, errors.New("capacity cleanup is unsupported")
	}
	ids, err := c.CapacityMessageIDs(ctx, t, q)
	if err != nil {
		return n, err
	}
	// CapacityMessageIDs rejects unsettled processing; normal inserts wait for
	// their batch to be persisted before the PostgreSQL completion marker.
	for table, messages := range map[string][]string{"iot_raw_message": q.RawIDs, "iot_telemetry": ids} {
		where := []string{}
		if len(messages) > 0 {
			where = append(where, "message_id IN ("+capacityStrings(messages)+")")
		}
		if len(q.RemoveDevices) > 0 {
			where = append(where, "device_id IN ("+capacityStrings(q.RemoveDevices)+")")
		}
		if len(where) == 0 {
			continue
		}
		cluster := ""
		if r.opts.Cluster != "" {
			table += "_local"
			cluster = " ON CLUSTER " + r.opts.Cluster
		}
		query := fmt.Sprintf("ALTER TABLE %s%s DELETE WHERE tenant_id=%s AND product_id=%s AND device_id IN (%s) AND (%s) SETTINGS mutations_sync=2", table, cluster, capacityQuote(t), capacityQuote(q.Product), capacityStrings(q.Devices), strings.Join(where, " OR "))
		if _, err = r.query(ctx, query, nil); err != nil {
			return n, err
		}
	}
	return c.CleanupCapacityData(ctx, t, q)
}

func capacityQuote(v string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}
func capacityStrings(v []string) string {
	out := make([]string, len(v))
	for i, s := range v {
		out[i] = capacityQuote(s)
	}
	return strings.Join(out, ",")
}
