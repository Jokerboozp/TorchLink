package backup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

const dutyAttachmentBucket = "iot-duty-attachments"

var dutyTables = []string{"duty_receipt", "duty_station", "duty_team", "duty_shift_template", "duty_roster", "duty_run", "duty_record", "duty_item", "duty_item_event", "duty_handover", "duty_handover_revision", "duty_ai_job", "duty_notification", "duty_attachment", "duty_business_event"}

// Duty uses separate artifacts from knowledge, so previous FULL backups retain
// their original schema and restore contract.
func (s *Service) exportDuty(ctx context.Context, dir string, m *Manifest) ([]string, error) {
	tx, err := s.beginBackupRead(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	dataPath := filepath.Join(dir, "duty-postgres.jsonl.gz")
	schema, counts, err := exportDataTables(ctx, tx, dutyTables, dataPath)
	if err != nil {
		return nil, fmt.Errorf("duty database: %w", err)
	}
	schemaPath := filepath.Join(dir, "duty-schema.json")
	if err = writeJSON(schemaPath, schema); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT body->'attachment'->>'objectKey' FROM duty_attachment ORDER BY 1`)
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
			return nil, errors.New("invalid duty attachment reference")
		}
		refs = append(refs, knowledgeObject{Bucket: dutyAttachmentBucket, Key: key})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	objectPath := filepath.Join(dir, "duty-objects.tar.gz")
	bytes, err := s.exportSnapshotObjects(ctx, refs, objectPath)
	if err != nil {
		return nil, fmt.Errorf("duty attachments: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	m.Components["duty"] = map[string]any{"status": "included", "tables": counts}
	m.Components["dutyObjects"] = map[string]any{"status": "included", "objects": len(refs), "bytes": bytes}
	return []string{schemaPath, dataPath, objectPath}, nil
}
