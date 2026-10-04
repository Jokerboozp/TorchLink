-- Backfills offline_check_at with model.DeviceState.OfflineCheckAt.
UPDATE device_state SET offline_check_at = CASE
  WHEN last_seen_at = 0 THEN 0
  WHEN body->>'dataStatus' = 'SILENT'
   AND body->>'businessStatus' = CASE WHEN body->>'connectionStatus' = 'CONNECTED' THEN 'SUSPECTED_OFFLINE' ELSE 'OFFLINE' END
   AND COALESCE((body->>'offlineAt')::bigint, 0) = last_seen_at + (COALESCE((body->>'reportIntervalSec')::bigint, 0) + COALESCE((body->>'offlineToleranceSec')::bigint, 0)) * 1000
  THEN 0
  ELSE last_seen_at + (COALESCE((body->>'reportIntervalSec')::bigint, 0) + COALESCE((body->>'offlineToleranceSec')::bigint, 0)) * 1000
END;
