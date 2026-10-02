package rawstore

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"

	"iot-platform/internal/model"
)

// CapacityRawObjectLimit bounds the expanded content inspected for legacy
// archive cleanup; unknown larger objects are preserved.
const CapacityRawObjectLimit = 8 << 20

// ValidateCapacityRawObjectScope requires an indexed raw record authorized by
// the exact ledger IDs or by an exclusive fixture device in this batch.
func ValidateCapacityRawObjectScope(tenant string, q model.CapacityCleanupBatch, idx model.RawArchiveIndex) error {
	if tenant == "" || idx.TenantID != tenant || q.Product == "" || idx.ProductID != q.Product || idx.DeviceID == "" || idx.MessageID == "" || idx.ObjectBucket == "" || idx.ObjectKey == "" || idx.ObjectOffset != 0 || !slices.Contains(q.Devices, idx.DeviceID) || !(slices.Contains(q.RawIDs, idx.MessageID) || slices.Contains(q.RemoveDevices, idx.DeviceID)) {
		return model.ErrResourceInUse
	}
	return nil
}

// ValidateCapacityRawObject verifies the complete gzip content contains one
// exact authorized record. An offset of zero alone never establishes ownership.
// Reader errors remain available to adapters for missing-object retry handling.
func ValidateCapacityRawObject(ctx context.Context, reader io.Reader, tenant string, q model.CapacityCleanupBatch, idx model.RawArchiveIndex) error {
	if err := ValidateCapacityRawObjectScope(tenant, q, idx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	compressed := &io.LimitedReader{R: reader, N: 2*CapacityRawObjectLimit + 1}
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.Join(model.ErrResourceInUse, err)
	}
	defer gz.Close()
	// Reading through EOF verifies the checksum and all concatenated members.
	content, err := io.ReadAll(io.LimitReader(gz, CapacityRawObjectLimit+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.Join(model.ErrResourceInUse, err)
	}
	if len(content) > CapacityRawObjectLimit || compressed.N == 0 {
		return model.ErrResourceInUse
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	var raw model.RawMessage
	if err = decoder.Decode(&raw); err != nil || raw.TenantID != tenant || raw.ProductID != idx.ProductID || raw.DeviceID != idx.DeviceID || raw.MessageID != idx.MessageID {
		return model.ErrResourceInUse
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return model.ErrResourceInUse
	}
	return ctx.Err()
}
