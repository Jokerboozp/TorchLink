package edgeagent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var ErrQuarantined = errors.New("raw is retained in the rejected queue; operator review is required")

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
		if !strings.HasSuffix(entry.Name(), ".json") && !strings.HasSuffix(entry.Name(), ".rejected") {
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
func (q *Queue) Put(raw model.RawMessage) error {
	if raw.TenantID == "" || raw.MessageID == "" {
		return errors.New("raw identity is required")
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return errors.New("raw exceeds 1 MiB")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	target := filepath.Join(q.root, queueName(raw))
	if q.closed {
		return os.ErrClosed
	}
	if _, err := os.Stat(target + ".rejected"); err == nil {
		return ErrQuarantined
	} else if !os.IsNotExist(err) {
		return err
	}
	if old, err := os.ReadFile(target); err == nil {
		if !bytes.Equal(old, b) {
			return model.ErrRawConflict
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(q.root)
	if err != nil {
		return err
	}
	items := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".rejected") {
			items++
		}
	}
	if items >= q.maxItems || q.used+int64(len(b)) > q.maxBytes {
		return ErrQueueFull
	}
	if err := atomicFile(target, b); err != nil {
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
	b, err := os.ReadFile(filepath.Join(q.root, entries[0]))
	if err != nil {
		return raw, false, err
	}
	if err = json.Unmarshal(b, &raw); err != nil {
		return raw, false, fmt.Errorf("corrupt queue entry %s: %w", entries[0], err)
	}
	if queueName(raw) != entries[0] {
		return raw, false, errors.New("queue identity checksum mismatch")
	}
	return raw, true, nil
}
func (q *Queue) Ack(raw model.RawMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
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
	return nil
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
