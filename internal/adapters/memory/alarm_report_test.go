package memory

import (
	"testing"

	"iot-platform/internal/repositorytest"
)

func TestAlarmReports(t *testing.T) { repositorytest.AlarmReports(t, NewRepository()) }

func TestDeviceOverview(t *testing.T) { repositorytest.DeviceOverview(t, NewRepository()) }

func TestAIRuns(t *testing.T) { repositorytest.AIRuns(t, NewRepository()) }

func TestAIAnalysisOutcomes(t *testing.T) { repositorytest.AIAnalysisOutcomes(t, NewRepository()) }
