package postgres

import (
	"iot-platform/internal/repositorytest"
	"testing"
)

func TestVideoMediaRetries(t *testing.T) { repositorytest.VideoMediaRetries(t, testRepository(t)) }
