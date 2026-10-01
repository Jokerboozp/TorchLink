package memory

import (
	"testing"

	"iot-platform/internal/repositorytest"
)

func TestFireSafetyStore(t *testing.T) {
	repositorytest.FireSafety(t, NewRepository())
}
