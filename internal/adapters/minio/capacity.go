package minioadapter

import (
	"context"
	"errors"

	"github.com/minio/minio-go/v7"
	"iot-platform/internal/adapters/rawstore"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.CapacityRawObjectCleaner = (*Archive)(nil)

// DeleteCapacityRawObject only deletes a gzip object containing one exact,
// authorized raw record. Legacy batch objects remain intact even when the
// requested record is the first one (objectOffset == 0).
func (a *Archive) DeleteCapacityRawObject(ctx context.Context, tenant string, q model.CapacityCleanupBatch, idx model.RawArchiveIndex) error {
	if err := rawstore.ValidateCapacityRawObjectScope(tenant, q, idx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	object, err := a.client.GetObject(ctx, idx.ObjectBucket, idx.ObjectKey, minio.GetObjectOptions{})
	if err != nil {
		if capacityObjectMissing(err) {
			return nil
		}
		return err
	}
	defer object.Close()
	if err = rawstore.ValidateCapacityRawObject(ctx, object, tenant, q, idx); err != nil {
		if capacityObjectMissing(err) {
			return nil
		}
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	err = a.client.RemoveObject(ctx, idx.ObjectBucket, idx.ObjectKey, minio.RemoveObjectOptions{})
	if capacityObjectMissing(err) {
		return nil
	}
	return err
}

func capacityObjectMissing(err error) bool {
	var response minio.ErrorResponse
	return errors.As(err, &response) && response.Code == "NoSuchKey"
}
