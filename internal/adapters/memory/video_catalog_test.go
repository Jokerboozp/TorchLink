package memory

import (
	"iot-platform/internal/repositorytest"
	"testing"
)

func TestCatalogCamera(t *testing.T) { repositorytest.CatalogCamera(t, NewRepository()) }
