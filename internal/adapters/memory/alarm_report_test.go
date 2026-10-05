package memory

import (
	"testing"

	"iot-platform/internal/repositorytest"
)

func TestAlarmReports(t *testing.T) { repositorytest.AlarmReports(t, NewRepository()) }

func TestDeviceOverview(t *testing.T) { repositorytest.DeviceOverview(t, NewRepository()) }
