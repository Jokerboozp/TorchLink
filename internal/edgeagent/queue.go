package edgeagent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/model"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrQuarantined = errors.New("raw is quarantined; operator review is required")

var ErrQueueFull = errors.New("edge queue is full; data was not acknowledged")

type Queue struct {
	mu             sync.Mutex
	root           string
	maxBytes, used int64
	maxItems       int
	lock           *os.File
	closed         bool
}

func OpenQueue(root string, maxBytes int64, maxItems int) (*Queue, error) {
	if maxBytes < 1 || maxItems < 1 {
		return nil, errors.New("invalid queue limits")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	lock, err := lockQueue(filepath.Join(root, ".lock"))
	if err != nil {
		return nil, err
	}
	q := &Queue{root: root, maxBytes: maxBytes, maxItems: maxItems, lock: lock}
	entries, err := os.ReadDir(root)
	if err != nil {
		q.Close()
		return nil, err
	}
	for _, entry := range entries {
		if !queueEntry(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			q.Close()
			return nil, errors.New("invalid queue entry")
		}
		q.used += info.Size()
	}
	return q, nil
}
func (q *Queue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil
	}
	q.closed = true
	return q.lock.Close()
}
func queueName(raw model.RawMessage) string {
	h := sha256.Sum256([]byte(raw.TenantID + "\x00" + raw.MessageID))
	return hex.EncodeToString(h[:]) + ".json"
}
func (q *Queue) Put(raw model.RawMessage) error { return q.put(raw, false) }

// PutReceived stamps the first transport reception and preserves it on an
// identical retransmission. All other fields must still match byte-for-byte.
func (q *Queue) PutReceived(raw model.RawMessage) error { return q.put(raw, true) }
func (q *Queue) put(raw model.RawMessage, stamp bool) error {
	if raw.TenantID == "" || raw.MessageID == "" {
		return errors.New("raw identity is required")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	target := filepath.Join(q.root, queueName(raw))
	if q.closed {
		return os.ErrClosed
	}
	if stamp {
		if old, err := os.ReadFile(target); err == nil {
			var saved model.RawMessage
			if json.Unmarshal(old, &saved) != nil {
				return ErrQuarantined
			}
			raw.ReceivedAt = saved.ReceivedAt
		} else if os.IsNotExist(err) {
			raw.ReceivedAt = time.Now().UnixMilli()
		} else {
			return err
		}
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return errors.New("raw exceeds 1 MiB")
	}
	for _, suffix := range []string{".rejected", ".corrupt"} {
		if _, err := os.Lstat(target + suffix); err == nil {
			return ErrQuarantined
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if old, err := os.ReadFile(target); err == nil {
		if !bytes.Equal(old, b) {
			return model.ErrRawConflict
		}
		// A prior write may have renamed successfully but failed its directory
		// flush. Retrying the receipt must complete that flush before success.
		return syncDirectory(q.root)
	} else if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(q.root)
	if err != nil {
		return err
	}
	items := 0
	for _, entry := range entries {
		if queueEntry(entry.Name()) {
			items++
		}
	}
	if items >= q.maxItems || q.used+int64(len(b)) > q.maxBytes {
		return ErrQueueFull
	}
	if err := atomicFile(target, b); err != nil {
		// Keep capacity accounting correct after a post-rename flush failure.
		if info, statErr := os.Stat(target); statErr == nil {
			q.used += info.Size()
		}
		return err
	}
	q.used += int64(len(b))
	return nil
}
func (q *Queue) entries() ([]string, error) {
	all, err := os.ReadDir(q.root)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range all {
		if strings.HasSuffix(e.Name(), ".json") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
func (q *Queue) Next() (model.RawMessage, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var raw model.RawMessage
	if q.closed {
		return raw, false, os.ErrClosed
	}
	entries, err := q.entries()
	if err != nil || len(entries) == 0 {
		return raw, false, err
	}
	path := filepath.Join(q.root, entries[0])
	info, err := os.Lstat(path)
	if err != nil {
		return raw, false, err
	}
	if !info.Mode().IsRegular() {
		return raw, false, errors.New("queue entry is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return raw, false, err
	}
	b, readErr := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	closeErr := f.Close()
	if readErr != nil {
		return raw, false, readErr
	}
	if closeErr != nil {
		return raw, false, closeErr
	}
	reason := ""
	if len(b) > 1<<20 {
		reason = "entry exceeds 1 MiB"
	} else if json.Unmarshal(b, &raw) != nil {
		reason = "invalid JSON"
	} else if raw.TenantID == "" || raw.MessageID == "" || queueName(raw) != entries[0] {
		reason = "identity checksum mismatch"
	}
	if reason != "" {
		// Retain the exact bytes in a separate quarantine. Operator retry of
		// server rejections must never requeue unreadable or mismatched data.
		if _, err := os.Lstat(path + ".corrupt"); err == nil {
			return raw, false, errors.New("corrupt quarantine already exists")
		} else if !os.IsNotExist(err) {
			return raw, false, err
		}
		if err := os.Rename(path, path+".corrupt"); err != nil {
			return raw, false, err
		}
		if err := syncDirectory(q.root); err != nil {
			return raw, false, err
		}
		return model.RawMessage{}, false, fmt.Errorf("queue entry %s quarantined: %s", entries[0], reason)
	}
	return raw, true, nil
}
func (q *Queue) Ack(raw model.RawMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return os.ErrClosed
	}
	path := filepath.Join(q.root, queueName(raw))
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	q.used -= info.Size()
	return syncDirectory(q.root)
}
func (q *Queue) Depth() int { q.mu.Lock(); defer q.mu.Unlock(); v, _ := q.entries(); return len(v) }
func atomicFile(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

// Reject retains permanent failures on disk while allowing later messages to upload.
func (q *Queue) Reject(raw model.RawMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return os.ErrClosed
	}
	path := filepath.Join(q.root, queueName(raw))
	if err := os.Rename(path, path+".rejected"); err != nil {
		return err
	}
	return syncDirectory(q.root)
}
func (q *Queue) Rejected() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	entries, _ := os.ReadDir(q.root)
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".rejected") {
			count++
		}
	}
	return count
}

// RetryRejected is an explicit operator action after correcting assignments or data.
func (q *Queue) RetryRejected() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return os.ErrClosed
	}
	entries, err := os.ReadDir(q.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json.rejected") {
			source := filepath.Join(q.root, entry.Name())
			target := strings.TrimSuffix(source, ".rejected")
			if _, err := os.Stat(target); err == nil {
				return errors.New("active queue already contains rejected id")
			}
			if err := os.Rename(source, target); err != nil {
				return err
			}
		}
	}
	return syncDirectory(q.root)
}

func queueEntry(name string) bool {
	return strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".rejected") || strings.HasSuffix(name, ".corrupt")
}
func (q *Queue) Corrupt() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	entries, _ := os.ReadDir(q.root)
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".corrupt") {
			count++
		}
	}
	return count
}
