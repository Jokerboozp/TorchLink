package memory

import (
	"testing"

	"iot-platform/internal/repositorytest"
)

func TestAlarmReports(t *testing.T) { repositorytest.AlarmReports(t, NewRepository()) }
