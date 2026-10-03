package notify_test

import (
	"testing"

	"iot-platform/internal/notify"
	"iot-platform/internal/notify/notifytest"
)

func TestMemoryStoreContract(t *testing.T) { notifytest.StoreContract(t, notify.NewMemoryStore()) }
