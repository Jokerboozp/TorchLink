package postgres

import (
	"testing"

	"iot-platform/internal/repositorytest"
)

func TestRawFiltersPostgres(t *testing.T) {
	repositorytest.RawFilters(t, testRepository(t))
}
