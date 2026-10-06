-- Duration timers belong to their rule: remove timers left by rules deleted
-- before deletion became transactional, then let the database enforce it.
DELETE FROM alarm_rule_pending p
WHERE NOT EXISTS (SELECT 1 FROM alarm_rule r WHERE r.tenant_id = p.tenant_id AND r.id = p.rule_id);

ALTER TABLE alarm_rule_pending
  ADD CONSTRAINT alarm_rule_pending_rule_fk FOREIGN KEY (tenant_id, rule_id)
  REFERENCES alarm_rule (tenant_id, id) ON DELETE CASCADE;
