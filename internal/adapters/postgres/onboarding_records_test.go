package postgres

import (
	"iot-platform/internal/repositorytest"
	"testing"
)

func TestOnboardingRecordCAS(t *testing.T) { repositorytest.OnboardingRecords(t, testRepository(t)) }
