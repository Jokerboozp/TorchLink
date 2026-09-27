//go:build !windows

package mqttadapter

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// lockDirectory takes an exclusive, non-blocking advisory lock that the OS
// releases when the process exits, so a crash never leaves a stale lock.
func lockDirectory(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrInboxInUse
		}
		return nil, err
	}
	return f, nil
}

func unlockDirectory(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	_ = f.Close()
}
