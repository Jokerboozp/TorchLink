//go:build windows

package mqttadapter

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockDirectory takes an exclusive, non-blocking lock that Windows releases
// when the process exits.
func lockDirectory(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	if err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped); err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrInboxInUse
		}
		return nil, err
	}
	return f, nil
}

func unlockDirectory(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
	_ = f.Close()
}
