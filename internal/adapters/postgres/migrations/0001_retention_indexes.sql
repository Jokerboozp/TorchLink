-- migrate:no-transaction
-- Time indexes used by the daily retention job. CONCURRENTLY keeps ingest
-- writing to these large tables while the indexes build.
ALTER TABLE raw_ingest_reservation ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now()
;
CREATE INDEX CONCURRENTLY IF NOT EXISTS raw_ingest_reservation_created_idx ON raw_ingest_reservation(created_at)
;
CREATE INDEX CONCURRENTLY IF NOT EXISTS raw_archive_index_received_idx ON raw_archive_index(received_at)
;
CREATE INDEX CONCURRENTLY IF NOT EXISTS raw_message_log_received_idx ON raw_message_log(received_at)
;
CREATE INDEX CONCURRENTLY IF NOT EXISTS standard_message_processed_idx ON standard_message(processed_at) WHERE processed_at > 0
;
CREATE INDEX CONCURRENTLY IF NOT EXISTS device_state_event_created_idx ON device_state_event(created_at)
;
CREATE INDEX CONCURRENTLY IF NOT EXISTS alarm_record_settled_idx ON alarm_record(last_triggered_at) WHERE status IN ('CLOSED','RECOVERED','SUPPRESSED')
;
CREATE INDEX CONCURRENTLY IF NOT EXISTS audit_log_created_idx ON audit_log(created_at)
;
CREATE INDEX CONCURRENTLY IF NOT EXISTS ai_tool_call_log_created_idx ON ai_tool_call_log(created_at)
;
CREATE INDEX CONCURRENTLY IF NOT EXISTS video_alarm_event_time_idx ON video_alarm_event(event_time)
;
