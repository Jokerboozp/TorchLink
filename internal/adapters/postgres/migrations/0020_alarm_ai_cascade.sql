-- AI analyses and analysis jobs belong to their alarm and are removed with it
-- by alarm retention or deletion. NOT VALID keeps the legacy rows that could
-- not be matched to an alarm (tenant __legacy_orphaned__, see schema.sql)
-- while every new row is checked.
ALTER TABLE alarm_ai_analysis
  ADD CONSTRAINT alarm_ai_analysis_alarm_fk FOREIGN KEY (tenant_id, alarm_id)
  REFERENCES alarm_record (tenant_id, id) ON DELETE CASCADE NOT VALID;
ALTER TABLE alarm_analysis_job
  ADD CONSTRAINT alarm_analysis_job_alarm_fk FOREIGN KEY (tenant_id, alarm_id)
  REFERENCES alarm_record (tenant_id, id) ON DELETE CASCADE NOT VALID;

-- Retention purges finished commands by age.
CREATE INDEX IF NOT EXISTS device_command_created_idx ON device_command (created_at);
