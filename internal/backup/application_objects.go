package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

var applicationAttachmentBuckets = map[string]string{"RESPONSE_ATTACHMENT": "iot-response-attachments", "MAINTENANCE_ATTACHMENT": "iot-maintenance-attachments", model.DataQualityAttachmentKind: "iot-quality-attachments"}

type applicationObjectReference struct {
	Tenant string
	Object knowledgeObject
}

func applicationObjectReferences(ctx context.Context, tx pgx.Tx, schema string) (map[string]applicationObjectReference, error) {
	ident := pgx.Identifier{"analysis_document"}.Sanitize()
	if schema != "" {
		ident = pgx.Identifier{schema, "analysis_document"}.Sanitize()
	}
	kinds := []string{}
	for kind := range applicationAttachmentBuckets {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	rows, err := tx.Query(ctx, "SELECT tenant_id,body FROM "+ident+" WHERE kind='config' AND application_kind=ANY($1::text[]) ORDER BY tenant_id,id", kinds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := map[string]applicationObjectReference{}
	for rows.Next() {
		var tenant string
		var raw []byte
		if err = rows.Scan(&tenant, &raw); err != nil {
			return nil, err
		}
		var config model.AnalysisConfigRevision
		if err = json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
		var body struct {
			ObjectKey   string `json:"objectKey"`
			StorageKey  string `json:"storageKey"`
			SHA256      string `json:"sha256"`
			Size        int64  `json:"size"`
			ContentType string `json:"contentType"`
		}
		if err = json.Unmarshal(config.Body, &body); err != nil {
			return nil, err
		}
		if config.Kind == model.DataQualityAttachmentKind {
			body.ObjectKey = body.StorageKey
		}
		bucket := applicationAttachmentBuckets[config.Kind]
		if config.TenantID != tenant || bucket == "" || body.ObjectKey == "" || body.Size < 0 || (len(body.SHA256) != 64 && !(config.Kind == model.DataQualityAttachmentKind && body.SHA256 == "")) {
			return nil, errors.New("invalid application attachment reference")
		}
		ref := applicationObjectReference{Tenant: tenant, Object: knowledgeObject{Bucket: bucket, Key: body.ObjectKey, SHA256: body.SHA256, Size: body.Size, ContentType: body.ContentType}}
		ref.Object.OriginalHashStatus = "RECORDED"
		if body.SHA256 == "" {
			ref.Object.OriginalHashStatus = "UNKNOWN_AT_UPLOAD"
		}
		key := analytics.RestoredObjectID(bucket, body.ObjectKey)
		if previous, ok := refs[key]; ok && (previous.Tenant != tenant || previous.Object != ref.Object) {
			return nil, errors.New("conflicting immutable attachment metadata")
		}
		refs[key] = ref
	}
	return refs, rows.Err()
}

func (s *Service) exportApplicationObjects(ctx context.Context, tx pgx.Tx, path string) (objects int, bytes int64, unknownHashes int, err error) {
	references, err := applicationObjectReferences(ctx, tx, "")
	if err != nil {
		return 0, 0, 0, err
	}
	keys := make([]string, 0, len(references))
	for key := range references {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	refs := make([]knowledgeObject, 0, len(keys))
	err = writeGzip(path, func(w io.Writer) error {
		archive := tar.NewWriter(w)
		for i, key := range keys {
			source := references[key]
			ref := source.Object
			ref.Entry = fmt.Sprintf("objects/%012d", i)
			bucket, objectKey := ref.Bucket, ref.Key
			var raw []byte
			e := tx.QueryRow(ctx, `SELECT body FROM analysis_document WHERE tenant_id=$1 AND kind='restored-object' AND id=$2`, source.Tenant, key).Scan(&raw)
			if e == nil {
				var location model.AnalysisRestoredObjectLocation
				if json.Unmarshal(raw, &location) != nil || location.TenantID != source.Tenant || location.SourceBucket != ref.Bucket || location.SourceKey != ref.Key || (ref.SHA256 != "" && location.SHA256 != ref.SHA256) || location.Size != ref.Size {
					return errors.New("invalid restored object overlay")
				}
				if ref.SHA256 == "" {
					ref.SHA256 = location.SHA256
				}
				bucket, objectKey = location.Bucket, location.Key
			} else if !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			object, e := s.store.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
			if e != nil {
				return e
			}
			info, e := object.Stat()
			if e != nil {
				object.Close()
				return e
			}
			if info.Size != ref.Size {
				object.Close()
				return errors.New("application object size differs from immutable metadata")
			}
			if e = archive.WriteHeader(&tar.Header{Name: ref.Entry, Mode: 0600, Size: ref.Size, Typeflag: tar.TypeReg}); e != nil {
				object.Close()
				return e
			}
			hash := sha256.New()
			n, e := io.Copy(io.MultiWriter(archive, hash), object)
			object.Close()
			if e != nil {
				return e
			}
			if n != ref.Size || (ref.SHA256 != "" && hex.EncodeToString(hash.Sum(nil)) != ref.SHA256) {
				return errors.New("application attachment checksum differs from immutable metadata")
			}
			ref.SHA256 = hex.EncodeToString(hash.Sum(nil))
			if ref.OriginalHashStatus == "UNKNOWN_AT_UPLOAD" {
				unknownHashes++
			}
			bytes += n
			refs = append(refs, ref)
		}
		raw, e := json.Marshal(refs)
		if e != nil {
			return e
		}
		if e = archive.WriteHeader(&tar.Header{Name: "index.json", Mode: 0600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); e != nil {
			return e
		}
		if _, e = archive.Write(raw); e != nil {
			return e
		}
		return archive.Close()
	})
	return len(refs), bytes, unknownHashes, err
}
