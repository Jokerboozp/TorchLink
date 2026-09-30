package backup

import (
	"context"
	"errors"
	"iot-platform/internal/adapters/postgres"
	"path/filepath"
)

const governanceAttachmentBucket = "iot-alarm-governance-attachments"

func (s *Service) exportGovernance(ctx context.Context, dir string, m *Manifest) ([]string, error) {
	tx, err := s.beginBackupRead(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	dataPath := filepath.Join(dir, "governance-postgres.jsonl.gz")
	schema, counts, err := exportTableSnapshot(ctx, tx, postgres.AlarmGovernanceDomainRestoreTables(), dataPath)
	if err != nil {
		return nil, err
	}
	schemaPath := filepath.Join(dir, "governance-schema.json")
	if err = writeJSON(schemaPath, schema); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT storage_key FROM (SELECT body->>'storageKey' AS storage_key FROM alarm_governance_attachment WHERE COALESCE(body->>'availability','AVAILABLE')<>'UNAVAILABLE' UNION SELECT body->'body'->>'storageKey' AS storage_key FROM alarm_governance_attachment_history WHERE COALESCE(body->'body'->>'availability','AVAILABLE')<>'UNAVAILABLE') refs WHERE COALESCE(storage_key,'')<>'' ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	refs := []knowledgeObject{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		if key == "" {
			rows.Close()
			return nil, errors.New("governance attachment storage reference missing")
		}
		refs = append(refs, knowledgeObject{Bucket: governanceAttachmentBucket, Key: key})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	objectPath := filepath.Join(dir, "governance-objects.tar.gz")
	bytes, err := s.exportSnapshotObjects(ctx, refs, objectPath)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	m.Components["governance"] = map[string]any{"status": "included", "tables": counts, "scope": "governance resources, immutable alarm observations and source versions", "analysisDocuments": "application"}
	m.Components["governanceObjects"] = map[string]any{"status": "included", "objects": len(refs), "bytes": bytes}
	return []string{schemaPath, dataPath, objectPath}, nil
}
