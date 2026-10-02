package memory

import (
	"iot-platform/internal/repositorytest"
	"testing"
)

func TestOnboardingRecordCAS(t *testing.T) { repositorytest.OnboardingRecords(t, NewRepository()) }
