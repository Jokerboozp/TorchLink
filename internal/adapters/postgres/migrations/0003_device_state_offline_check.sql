-- migrate:no-transaction
-- The offline scan reads only devices whose check time has passed.
ALTER TABLE device_state ADD COLUMN IF NOT EXISTS offline_check_at bigint NOT NULL DEFAULT 0
;
CREATE INDEX CONCURRENTLY IF NOT EXISTS device_state_offline_check_idx ON device_state(offline_check_at) WHERE offline_check_at > 0
;
