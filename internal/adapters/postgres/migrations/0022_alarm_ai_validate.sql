-- Analyses whose alarm no longer exists (unmatched legacy rows of tenant
-- __legacy_orphaned__, and analyses left by alarms deleted before migration
-- 0020) move to alarm_ai_analysis_orphaned, which no request reads, so the
-- foreign keys added NOT VALID in 0020 can be validated. Analysis jobs of
-- missing alarms are finished or abandoned queue entries and are dropped.
CREATE TABLE IF NOT EXISTS alarm_ai_analysis_orphaned (LIKE alarm_ai_analysis INCLUDING DEFAULTS);
ALTER TABLE alarm_ai_analysis_orphaned ADD COLUMN IF NOT EXISTS orphaned_at timestamptz NOT NULL DEFAULT now();
INSERT INTO alarm_ai_analysis_orphaned
SELECT analysis.*, now() FROM alarm_ai_analysis AS analysis
WHERE NOT EXISTS (SELECT 1 FROM alarm_record AS alarm WHERE alarm.tenant_id=analysis.tenant_id AND alarm.id=analysis.alarm_id);
DELETE FROM alarm_ai_analysis AS analysis
WHERE NOT EXISTS (SELECT 1 FROM alarm_record AS alarm WHERE alarm.tenant_id=analysis.tenant_id AND alarm.id=analysis.alarm_id);
DELETE FROM alarm_analysis_job AS job
WHERE NOT EXISTS (SELECT 1 FROM alarm_record AS alarm WHERE alarm.tenant_id=job.tenant_id AND alarm.id=job.alarm_id);
ALTER TABLE alarm_ai_analysis VALIDATE CONSTRAINT alarm_ai_analysis_alarm_fk;
ALTER TABLE alarm_analysis_job VALIDATE CONSTRAINT alarm_analysis_job_alarm_fk;
