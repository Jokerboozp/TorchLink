package memory

import (
	"iot-platform/internal/repositorytest"
	"testing"
)

func TestDeviceShadow(t *testing.T) { repositorytest.DeviceShadow(t, NewRepository()) }
