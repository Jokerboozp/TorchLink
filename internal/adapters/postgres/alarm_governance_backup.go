package postgres

import "regexp"

// AlarmGovernanceRestoreSchema is trusted application DDL used only inside an
// independent restore schema. Backups never supply executable SQL.
func AlarmGovernanceRestoreSchema() string {
	return alarmObservationSchema + "\n" + alarmGovernanceSchema + "\n" + analyticsSchema
}
func AlarmGovernanceRestoreTables() []string {
	matches := regexp.MustCompile(`(?i)CREATE TABLE IF NOT EXISTS ([a-z_]+)`).FindAllStringSubmatch(AlarmGovernanceRestoreSchema(), -1)
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
