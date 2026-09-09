package memory

import (
	"iot-platform/internal/repositorytest"
	"testing"
)

func TestDeviceShadow(t *testing.T) { repositorytest.DeviceShadow(t, NewRepository()) }

func TestNamedShadows(t *testing.T) { repositorytest.NamedShadows(t, NewRepository()) }
