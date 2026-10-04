package memory

import (
	"testing"

	"iot-platform/internal/repositorytest"
)

func TestStandardCompletion(t *testing.T) { repositorytest.StandardCompletion(t, NewRepository()) }
