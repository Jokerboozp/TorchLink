package postgres

import "regexp"

// AlarmGovernanceRestoreSchema is trusted application DDL used only inside an
// independent restore schema. Backups never supply executable SQL.
func AlarmGovernanceRestoreSchema() string {
	return alarmObservationSchema + "\n" + alarmGovernanceSchema + "\n" + analyticsSchema
}
func AlarmGovernanceRestoreTables() []string {
	return alarmGovernanceRestoreTables(AlarmGovernanceRestoreSchema())
}

// Combined backups use the application component as the sole owner of the
// shared analysis_document table. The domain schema retains all governance
// constraints, generated columns and observation sequences.
func AlarmGovernanceDomainRestoreSchema() string {
	return alarmObservationSchema + "\n" + alarmGovernanceSchema
}
func AlarmGovernanceDomainRestoreTables() []string {
	return alarmGovernanceRestoreTables(AlarmGovernanceDomainRestoreSchema())
}
func alarmGovernanceRestoreTables(schema string) []string {
	matches := regexp.MustCompile(`(?i)CREATE TABLE IF NOT EXISTS ([a-z_]+)`).FindAllStringSubmatch(schema, -1)
	out := []string{}
	seen := map[string]bool{}
	for _, m := range matches {
		if !seen[m[1]] {
			out = append(out, m[1])
			seen[m[1]] = true
		}
	}
	return out
}
