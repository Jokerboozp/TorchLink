package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (r *Repository) capacityCleaner() (ports.CapacityDataCleaner, error) {
	c, ok := r.Repository.(ports.CapacityDataCleaner)
	if !ok {
		return nil, errors.New("capacity cleanup is unsupported")
	}
	return c, nil
}

// CleanupCapacityData lets the relational store verify ownership and
// unfinished processing first, then removes the same devices' ClickHouse rows.
// A failed mutation is retried with the next cleanup; device IDs stay owned.
func (r *Repository) CleanupCapacityData(ctx context.Context, t string, q model.CapacityCleanupBatch) (model.CapacityCleanupCounts, error) {
	c, err := r.capacityCleaner()
	if err != nil {
		return model.CapacityCleanupCounts{}, err
	}
	n, err := c.CleanupCapacityData(ctx, t, q)
	if err != nil {
		return n, err
	}
	scope := ""
	if len(q.Devices) > 0 {
		scope = " AND device_id IN (" + capacityStrings(q.Devices) + ")"
	} else if q.RemoveProduct {
		// Once the dedicated product is gone, every remaining row of it is
		// test data; this also repairs rows left by an interrupted cleanup.
		if _, e := r.Repository.GetProduct(ctx, t, q.Product); !errors.Is(e, model.ErrNotFound) {
			return n, nil
		}
	} else {
		return n, nil
	}
	for _, table := range []string{"iot_raw_message", "iot_telemetry"} {
		cluster := ""
		if r.opts.Cluster != "" {
			table += "_local"
			cluster = " ON CLUSTER " + r.opts.Cluster
		}
		query := fmt.Sprintf("ALTER TABLE %s%s DELETE WHERE tenant_id=%s AND product_id=%s%s SETTINGS mutations_sync=2", table, cluster, quote(t), quote(q.Product), scope)
		if _, err = r.query(ctx, query, nil); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (r *Repository) ListCapacityFixtureProducts(ctx context.Context, tenant string) ([]model.CapacityFixtureProduct, error) {
	c, err := r.capacityCleaner()
	if err != nil {
		return nil, err
	}
	return c.ListCapacityFixtureProducts(ctx, tenant)
}

func (r *Repository) ListCapacityFixtureDevices(ctx context.Context, tenant, product, after string, limit int) ([]string, error) {
	c, err := r.capacityCleaner()
	if err != nil {
		return nil, err
	}
	return c.ListCapacityFixtureDevices(ctx, tenant, product, after, limit)
}

func capacityStrings(v []string) string {
	out := make([]string, len(v))
	for i, s := range v {
		out[i] = quote(s)
	}
	return strings.Join(out, ",")
}

var _ ports.CapacityDataCleaner = (*Repository)(nil)
