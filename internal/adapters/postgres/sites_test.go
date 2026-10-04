package postgres

import (
	"testing"

	"iot-platform/internal/repositorytest"
)

func TestSiteStore(t *testing.T) { repositorytest.Sites(t, testRepository(t)) }
