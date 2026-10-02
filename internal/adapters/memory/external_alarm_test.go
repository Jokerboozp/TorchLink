package memory

import (
	"testing"

	"iot-platform/internal/repositorytest"
)

func TestExternalAlarm(t *testing.T) {
	repositorytest.ExternalAlarm(t, NewRepository())
}
