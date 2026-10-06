//go:build !windows

package mqttadapter

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// lockDirectory takes an exclusive, non-blocking advisory lock that the OS
// releases when the process exits, so a crash never leaves a stale lock. The
// holder records its PID in the file so a second instance can name it.
func lockDirectory(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		holder := lockHolder(f)
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			if holder != "" {
				return nil, fmt.Errorf("%w (holder pid %s)", ErrInboxInUse, holder)
			}
			return nil, ErrInboxInUse
		}
		return nil, err
	}
	// Best effort: the lock itself is authoritative, the PID is only a hint.
	if err = f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	}
	return f, nil
}

// lockHolder returns the PID the current holder wrote, or "" when unknown.
func lockHolder(f *os.File) string {
	var buf [32]byte
	n, _ := f.ReadAt(buf[:], 0)
	pid := strings.TrimSpace(string(buf[:n]))
	if _, err := strconv.Atoi(pid); err != nil {
		return ""
	}
	return pid
}

func unlockDirectory(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	_ = f.Close()
}
