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

func TestTwinTopology(t *testing.T) { repositorytest.TwinTopology(t, NewRepository()) }

func TestProtocolRegistration(t *testing.T) { repositorytest.ProtocolRegistration(t, NewRepository()) }
