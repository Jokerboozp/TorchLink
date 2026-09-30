package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

func (s *Service) restoreApplicationObjects(ctx context.Context, tx pgx.Tx, schema, path, stage, restoreID string) (int64, error) {
	allowed := map[string]bool{}
	for _, bucket := range applicationAttachmentBuckets {
		allowed[bucket] = true
	}
	entries, refs, err := readPrivateObjects(path, stage, allowed)
	if err != nil {
		return 0, err
	}
	expected, err := applicationObjectReferences(ctx, tx, schema)
	if err != nil {
		return 0, err
	}
	if len(expected) != len(refs) {
		return 0, errors.New("application object references do not match immutable metadata")
	}
	for _, ref := range refs {
		v, ok := expected[analytics.RestoredObjectID(ref.Bucket, ref.Key)]
		if !ok || (v.Object.SHA256 != "" && v.Object.SHA256 != ref.SHA256) || v.Object.Size != ref.Size || ref.OriginalHashStatus != v.Object.OriginalHashStatus || !strings.HasPrefix(ref.Key, v.Tenant+"/") {
			return 0, errors.New("application object metadata checksum mismatch")
		}
	}
	if len(refs) == 0 {
		return 0, nil
	}
	if strings.TrimSpace(s.cfg.RestoreMinIOEndpoint) == "" || strings.EqualFold(strings.TrimRight(s.cfg.MinIOEndpoint, "/"), strings.TrimRight(s.cfg.RestoreMinIOEndpoint, "/")) {
		return 0, errors.New("application attachments require an independent MinIO restore endpoint")
	}
	store, err := minio.New(s.cfg.RestoreMinIOEndpoint, &minio.Options{Creds: credentials.NewStaticV4(s.cfg.RestoreMinIOAccessKey, s.cfg.RestoreMinIOSecretKey, ""), Secure: s.cfg.RestoreMinIOUseTLS})
	if err != nil {
		return 0, err
	}
	restoreHash := sha256.Sum256([]byte(restoreID))
	ident := pgx.Identifier{schema, "analysis_document"}.Sanitize()
	now := time.Now().UTC().UnixMilli()
	for _, ref := range refs {
		source := expected[analytics.RestoredObjectID(ref.Bucket, ref.Key)]
		bucketHash := sha256.Sum256([]byte(ref.Bucket))
		bucket := "iot-application-restore-" + hex.EncodeToString(restoreHash[:10]) + "-" + hex.EncodeToString(bucketHash[:4])
		if err = s.ensureBucket(ctx, store, bucket); err != nil {
			return 0, err
		}
		if _, err = store.FPutObject(ctx, bucket, ref.Key, entries[ref.Entry], minio.PutObjectOptions{ContentType: ref.ContentType}); err != nil {
			return 0, err
		}
		object, e := store.GetObject(ctx, bucket, ref.Key, minio.GetObjectOptions{})
		if e != nil {
			return 0, e
		}
		hash := sha256.New()
		size, e := io.Copy(hash, object)
		object.Close()
		if e != nil {
			return 0, e
		}
		if size != ref.Size || hex.EncodeToString(hash.Sum(nil)) != ref.SHA256 {
			return 0, errors.New("restored application attachment verification failed")
		}
		location := model.AnalysisRestoredObjectLocation{TenantID: source.Tenant, RestoreID: restoreID, SourceBucket: ref.Bucket, SourceKey: ref.Key, Bucket: bucket, Key: ref.Key, SHA256: ref.SHA256, Size: ref.Size}
		body, _ := json.Marshal(location)
		if _, err = tx.Exec(ctx, "INSERT INTO "+ident+"(tenant_id,kind,id,run_id,device_id,application_kind,resource_id,status,device_ids,version,created_at,body) VALUES($1,'restored-object',$2,'','','','','AVAILABLE','{}',1,$3,$4) ON CONFLICT(tenant_id,kind,id) DO UPDATE SET body=excluded.body,version=analysis_document.version+1,created_at=excluded.created_at", source.Tenant, analytics.RestoredObjectID(ref.Bucket, ref.Key), now, body); err != nil {
			return 0, err
		}
	}
	return int64(len(refs)), nil
}
