package memory

import (
	"iot-platform/internal/repositorytest"
	"testing"
)

func TestProtocolChildren(t *testing.T) { repositorytest.ProtocolChildren(t, NewRepository()) }
