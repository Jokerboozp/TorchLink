package local

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"iot-platform/internal/model"
)

type Archive struct{ root string }

func NewArchive(root string) (*Archive, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &Archive{root: root}, nil
}
func safe(v string) string {
	v = strings.ReplaceAll(v, "..", "_")
	v = strings.ReplaceAll(v, "/", "_")
	v = strings.ReplaceAll(v, "\\", "_")
	return v
}
func (a *Archive) PutRaw(_ context.Context, m model.RawMessage) (model.RawArchiveIndex, error) {
	t := time.UnixMilli(m.ReceivedAt).UTC()
	bucket := "iot-raw-archive"
	key := fmt.Sprintf("%s/%s/%s/%04d/%02d/%02d/%02d/raw-%02d-%s.jsonl.gz", safe(m.TenantID), safe(m.ProductID), safe(m.DeviceID), t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), safe(m.MessageID))
	full := filepath.Join(a.root, bucket, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return model.RawArchiveIndex{}, err
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if os.IsExist(err) {
		return a.index(m, bucket, key), nil
	}
	if err != nil {
		return model.RawArchiveIndex{}, err
	}
	gz := gzip.NewWriter(f)
	encErr := json.NewEncoder(gz).Encode(m)
	closeErr := gz.Close()
	fileErr := f.Close()
	if encErr != nil {
		return model.RawArchiveIndex{}, encErr
	}
	if closeErr != nil {
		return model.RawArchiveIndex{}, closeErr
	}
	if fileErr != nil {
		return model.RawArchiveIndex{}, fileErr
	}
	return a.index(m, bucket, key), nil
}
func (a *Archive) index(m model.RawMessage, bucket, key string) model.RawArchiveIndex {
	return model.RawArchiveIndex{MessageID: m.MessageID, TenantID: m.TenantID, ProductID: m.ProductID, DeviceID: m.DeviceID, Protocol: m.Protocol, PayloadFormat: m.PayloadFormat, ObjectBucket: bucket, ObjectKey: key, ObjectOffset: 0, PayloadHash: m.PayloadHash(), PayloadSize: len(m.Payload), ReceivedAt: m.ReceivedAt, ArchivedAt: time.Now().UnixMilli()}
}
func (a *Archive) GetRaw(_ context.Context, idx model.RawArchiveIndex) (model.RawMessage, error) {
	var m model.RawMessage
	f, err := os.Open(filepath.Join(a.root, safe(idx.ObjectBucket), filepath.FromSlash(idx.ObjectKey)))
	if err != nil {
		return m, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return m, err
	}
	defer gz.Close()
	err = json.NewDecoder(gz).Decode(&m)
	return m, err
}
func (a *Archive) PutObject(_ context.Context, bucket, key string, r io.Reader, _ int64, _ string) (string, error) {
	full := filepath.Join(a.root, safe(bucket), filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", err
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return fmt.Sprintf("local://%s/%s", bucket, key), err
}
func (a *Archive) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(a.root, safe(bucket), filepath.FromSlash(key)))
}
func (a *Archive) Health(context.Context) error { _, err := os.Stat(a.root); return err }
func (a *Archive) DeleteObject(_ context.Context, bucket, key string) error {
	base, err := filepath.Abs(filepath.Join(a.root, safe(bucket)))
	if err != nil {
		return err
	}
	target, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(key)))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid object key")
	}
	err = os.Remove(target)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
