package capacity

import (
	"os"
	"testing"
	"time"
)

// TestMain shortens the phase timing so end-to-end runs finish in seconds;
// plans in these tests use the reduced minimums.
func TestMain(m *testing.M) {
	minMeasure, minDrainTimeout, phaseLead = 3*time.Second, 3*time.Second, 500*time.Millisecond
	os.Exit(m.Run())
}
