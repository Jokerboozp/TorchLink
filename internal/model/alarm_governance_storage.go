package model

import "slices"

// Published configuration content is immutable; retirement changes only its
// lifecycle state and preserves the exact published semantic hash and body.
func GovernanceConfigurationMutationAllowed(old, next GovernanceDocument) bool {
	if !slices.Contains([]string{GovernanceTemplateKind, GovernanceSceneKind, GovernanceProfileKind}, old.Kind) {
		return true
	}
	prior, e := GovernanceBody[GovernanceConfiguration](old)
	if e != nil {
		return false
	}
	if prior.Builtin || prior.Status == "RETIRED" {
		return false
	}
	if prior.Status != "PUBLISHED" {
		return true
	}
	candidate, e := GovernanceBody[GovernanceConfiguration](next)
	if e != nil || candidate.Status != "RETIRED" {
		return false
	}
	candidate.Status = prior.Status
	return GovernanceHash(candidate) == GovernanceHash(prior)
}

func GovernanceImmutableAfterConfirmation(d GovernanceDocument) bool {
	if slices.Contains([]string{GovernanceEventKind, GovernanceReceiptKind, GovernanceReportKind, GovernanceAlarmLinkKind, GovernanceVerificationLinkKind, GovernanceCoverageKind, GovernanceActivityKind}, d.Kind) {
		return true
	}
	if slices.Contains([]string{GovernanceVerificationKind, GovernancePlanKind, GovernanceReviewKind}, d.Kind) && d.Status == "CONFIRMED" {
		return true
	}
	if d.Kind == GovernanceCauseKind && slices.Contains([]string{"CONFIRMED", "DISPUTED", "REJECTED"}, d.Status) {
		return true
	}
	return d.Kind == GovernanceRoundKind && d.Status != "ACTIVE"
}
