package memory

import (
	"iot-platform/internal/repositorytest"
	"testing"
)

func TestProtocolMarketAtomicReview(t *testing.T) { repositorytest.ProtocolMarket(t, NewRepository()) }
