package edgeagent

import (
	"golang.org/x/sys/windows"
	"os"
)

func lockQueue(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{}); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// File contents are flushed before atomic replacement. Windows does not expose
// directory fsync through os.File; this queue promises process-restart recovery.
func syncDirectory(string) error { return nil }
