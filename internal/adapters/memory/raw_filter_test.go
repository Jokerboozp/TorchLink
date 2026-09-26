package memory

import (
	"iot-platform/internal/repositorytest"
	"testing"
)

func TestRawFilters(t *testing.T) { repositorytest.RawFilters(t, NewRepository()) }
