package minioadapter

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"iot-platform/internal/model"
)

type Archive struct {
	client  *minio.Client
	ensured sync.Map
}

func New(endpoint, access, secret string, tls bool) (*Archive, error) {
	c, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: tls})
	if err != nil {
		return nil, err
	}
	return &Archive{client: c}, nil
}
func (a *Archive) ensure(ctx context.Context, bucket string) error {
	if _, ok := a.ensured.Load(bucket); ok {
		return nil
	}
	ok, err := a.client.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !ok {
		if err = a.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return err
		}
	}
	a.ensured.Store(bucket, struct{}{})
	return nil
}

// GetRaw reads objects written by the legacy per-message/batch MinIO archive.
// New ingest traffic is database-backed; this reader remains so old raw
// archive indexes can still be inspected, downloaded and replayed.
func (a *Archive) GetRaw(ctx context.Context, idx model.RawArchiveIndex) (model.RawMessage, error) {
	var m model.RawMessage
	o, err := a.client.GetObject(ctx, idx.ObjectBucket, idx.ObjectKey, minio.GetObjectOptions{})
	if err != nil {
		return m, err
	}
	defer o.Close()
	gz, err := gzip.NewReader(o)
	if err != nil {
		return m, err
	}
	defer gz.Close()
	decoder := json.NewDecoder(gz)
	for offset := int64(0); ; offset++ {
		if err = decoder.Decode(&m); err != nil {
			return model.RawMessage{}, err
		}
		if offset == idx.ObjectOffset || m.MessageID == idx.MessageID {
			return m, nil
		}
	}
}
func (a *Archive) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) (string, error) {
	if err := a.ensure(ctx, bucket); err != nil {
		return "", err
	}
	if size < 0 {
		data, err := io.ReadAll(r)
		if err != nil {
			return "", err
		}
		r = bytes.NewReader(data)
		size = int64(len(data))
	}
	_, err := a.client.PutObject(ctx, bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return fmt.Sprintf("minio://%s/%s", bucket, key), err
}
func (a *Archive) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	return a.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
}
func (a *Archive) Health(ctx context.Context) error { _, err := a.client.ListBuckets(ctx); return err }
func (a *Archive) DeleteObject(ctx context.Context, bucket, key string) error {
	return a.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
}
