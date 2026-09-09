package memory

import (
	"iot-platform/internal/repositorytest"
	"testing"
)

func TestAccessStatusDoesNotOverwriteConfiguration(t *testing.T) {
	repositorytest.AccessStatus(t, NewRepository())
}

func TestExecutionLeaseOwnership(t *testing.T) { repositorytest.ExecutionLease(t, NewRepository()) }

func TestRawReservationBeforeArchive(t *testing.T) { repositorytest.RawReservation(t, NewRepository()) }

func TestEdgeReadJobOwnership(t *testing.T) { repositorytest.EdgeReadJobs(t, NewRepository()) }
