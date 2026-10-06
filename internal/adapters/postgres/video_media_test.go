package postgres

import (
	"iot-platform/internal/repositorytest"
	"testing"
)

func TestVideoMediaRetries(t *testing.T) { repositorytest.VideoMediaRetries(t, testRepository(t)) }

func TestDevicePagesAndPlaySessions(t *testing.T) {
	repositorytest.DevicePagesAndPlaySessions(t, testRepository(t))
}

func TestRawPublishRetriesStopAtTheLimit(t *testing.T) {
	repositorytest.RawPublishRetriesStopAtTheLimit(t, testRepository(t))
}

func TestRuleDeleteRemovesDurationTimers(t *testing.T) {
	repositorytest.RuleDeleteRemovesDurationTimers(t, testRepository(t))
}

func TestKnowledgeDocLookupStaysInTenant(t *testing.T) {
	repositorytest.KnowledgeDocLookupStaysInTenant(t, testRepository(t))
}

func TestListContract(t *testing.T) { repositorytest.ListContract(t, testRepository(t)) }
