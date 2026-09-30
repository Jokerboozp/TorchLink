package analytics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.AnalysisRestoredObjectStore = (*Store)(nil)

func RestoredObjectID(bucket, key string) string {
	id, _ := AnalysisHash([]string{bucket, key})
	return id
}

func (s *Store) GetRestoredObjectLocation(ctx context.Context, tenant, bucket, key string) (out model.AnalysisRestoredObjectLocation, err error) {
	err = s.backend.Read(ctx, tenant, func(tx StorageTx) error {
		var e error
		out, e = load[model.AnalysisRestoredObjectLocation](tx, "restored-object", RestoredObjectID(bucket, key))
		return e
	})
	if err != nil {
		return model.AnalysisRestoredObjectLocation{}, err
	}
	if out.TenantID != tenant || out.SourceBucket != bucket || out.SourceKey != key || out.RestoreID == "" || !strings.HasPrefix(out.Bucket, "iot-application-restore-") || out.Key != key || !strings.HasPrefix(key, tenant+"/") || len(out.SHA256) != 64 || out.Size < 0 {
		return model.AnalysisRestoredObjectLocation{}, model.ErrAnalysisInvalid
	}
	return out, nil
}

// The caller authorizes the complete attachment revision before using this
// helper. A corrupt overlay fails closed; it cannot fall back to a live object.
func OpenAnalysisObject(ctx context.Context, store ports.AnalysisStore, archive ports.Archive, tenant, bucket, key string) (io.ReadCloser, error) {
	if archive == nil {
		return nil, ErrUnsupported
	}
	if restored, ok := store.(ports.AnalysisRestoredObjectStore); ok {
		location, err := restored.GetRestoredObjectLocation(ctx, tenant, bucket, key)
		if err == nil {
			return archive.GetObject(ctx, location.Bucket, location.Key)
		}
		if !errors.Is(err, model.ErrNotFound) {
			return nil, err
		}
	}
	return archive.GetObject(ctx, bucket, key)
}

// Verifies immutable upload metadata, or the restored artifact digest for old
// attachments whose original upload did not record one. Caller authorization
// happens before resolving or fetching an object.
func OpenVerifiedAnalysisObject(ctx context.Context, store ports.AnalysisStore, archive ports.Archive, tenant, bucket, key, expectedHash string, expectedSize, maxBytes int64) (io.ReadCloser, error) {
	if expectedSize < 0 || expectedSize > maxBytes || maxBytes <= 0 {
		return nil, model.ErrAnalysisInvalid
	}
	if overlay, ok := store.(ports.AnalysisRestoredObjectStore); ok {
		location, err := overlay.GetRestoredObjectLocation(ctx, tenant, bucket, key)
		if err == nil {
			if expectedHash != "" && location.SHA256 != expectedHash || location.Size != expectedSize {
				return nil, model.ErrAnalysisInvalid
			}
			expectedHash = location.SHA256
		} else if !errors.Is(err, model.ErrNotFound) {
			return nil, err
		}
	}
	reader, err := OpenAnalysisObject(ctx, store, archive, tenant, bucket, key)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	reader.Close()
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != expectedSize {
		return nil, model.ErrAnalysisInvalid
	}
	if expectedHash != "" {
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != expectedHash {
			return nil, model.ErrAnalysisInvalid
		}
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
