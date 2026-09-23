package durablequeue /* 声明 durablequeue 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"                       /* 执行当前语句并推进处理流程。 */
	"crypto/sha256"               /* 执行当前语句并推进处理流程。 */
	"encoding/hex"                /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"fmt"                         /* 执行当前语句并推进处理流程。 */
	"io"                          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"os"                          /* 执行当前语句并推进处理流程。 */
	"path/filepath"               /* 执行当前语句并推进处理流程。 */
	"sort"                        /* 执行当前语句并推进处理流程。 */
	"strings"                     /* 执行当前语句并推进处理流程。 */
	"sync"                        /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var ErrQuarantined = errors.New("raw is quarantined; operator review is required") /* 声明 ErrQuarantined。 */

var ErrQueueFull = errors.New("durable queue is full; data was not acknowledged") /* 声明 ErrQueueFull。 */

type Queue struct { /* 定义 Queue 类型。 */
	mu             sync.Mutex /* 执行当前语句并推进处理流程。 */
	root           string     /* 执行当前语句并推进处理流程。 */
	maxBytes, used int64      /* 执行当前语句并推进处理流程。 */
	maxItems       int        /* 执行当前语句并推进处理流程。 */
	lock           *os.File   /* 执行当前语句并推进处理流程。 */
	closed         bool       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func OpenQueue(root string, maxBytes int64, maxItems int) (*Queue, error) { /* 定义 OpenQueue 函数。 */
	if maxBytes < 1 || maxItems < 1 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("invalid queue limits") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := os.MkdirAll(root, 0700); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	lock, err := lockQueue(filepath.Join(root, ".lock")) /* 更新 err 的值。 */
	if err != nil {                                      /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	q := &Queue{root: root, maxBytes: maxBytes, maxItems: maxItems, lock: lock} /* 更新 q 的值。 */
	entries, err := os.ReadDir(root)                                            /* 更新 err 的值。 */
	if err != nil {                                                             /* 判断条件并选择处理分支。 */
		q.Close()       /* 执行当前语句并推进处理流程。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, entry := range entries { /* 循环处理当前数据。 */
		if !queueEntry(entry.Name()) { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		info, err := entry.Info()                   /* 更新 err 的值。 */
		if err != nil || !info.Mode().IsRegular() { /* 判断条件并选择处理分支。 */
			q.Close()                                     /* 执行当前语句并推进处理流程。 */
			return nil, errors.New("invalid queue entry") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		q.used += info.Size() /* 更新 q.used 的值。 */
	} /* 结束当前表达式或代码块。 */
	return q, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (q *Queue) Close() error { /* 定义 Close 函数。 */
	q.mu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer q.mu.Unlock() /* 安排函数结束时执行清理。 */
	if q.closed {       /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	q.closed = true       /* 更新 q.closed 的值。 */
	return q.lock.Close() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func queueName(raw model.RawMessage) string { /* 定义 queueName 函数。 */
	h := sha256.Sum256([]byte(raw.TenantID + "\x00" + raw.MessageID)) /* 更新 h 的值。 */
	return hex.EncodeToString(h[:]) + ".json"                         /* 返回当前处理结果。 */
}                                               /* 结束当前表达式或代码块。 */
func (q *Queue) Put(raw model.RawMessage) error { return q.put(raw, false) } /* 定义 Put 函数。 */

// PutReceived stamps the first transport reception and preserves it on an
// identical retransmission. All other fields must still match byte-for-byte.
func (q *Queue) PutReceived(raw model.RawMessage) error { return q.put(raw, true) } /* 定义 PutReceived 函数。 */
func (q *Queue) put(raw model.RawMessage, stamp bool) error { /* 定义 put 函数。 */
	if raw.TenantID == "" || raw.MessageID == "" { /* 判断条件并选择处理分支。 */
		return errors.New("raw identity is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	q.mu.Lock()                                     /* 执行当前语句并推进处理流程。 */
	defer q.mu.Unlock()                             /* 安排函数结束时执行清理。 */
	target := filepath.Join(q.root, queueName(raw)) /* 更新 target 的值。 */
	if q.closed {                                   /* 判断条件并选择处理分支。 */
		return os.ErrClosed /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if stamp { /* 判断条件并选择处理分支。 */
		if old, err := os.ReadFile(target); err == nil { /* 判断条件并选择处理分支。 */
			var saved model.RawMessage              /* 声明 saved。 */
			if json.Unmarshal(old, &saved) != nil { /* 判断条件并选择处理分支。 */
				return ErrQuarantined /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			raw.ReceivedAt = saved.ReceivedAt /* 更新 raw.ReceivedAt 的值。 */
		} else if os.IsNotExist(err) { /* 结束当前表达式或代码块。 */
			raw.ReceivedAt = time.Now().UnixMilli() /* 更新 raw.ReceivedAt 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	b, err := json.Marshal(raw) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(b) > 1<<20 { /* 判断条件并选择处理分支。 */
		return errors.New("raw exceeds 1 MiB") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, suffix := range []string{".rejected", ".corrupt"} { /* 循环处理当前数据。 */
		if _, err := os.Lstat(target + suffix); err == nil { /* 判断条件并选择处理分支。 */
			return ErrQuarantined /* 返回当前处理结果。 */
		} else if !os.IsNotExist(err) { /* 结束当前表达式或代码块。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if old, err := os.ReadFile(target); err == nil { /* 判断条件并选择处理分支。 */
		if !bytes.Equal(old, b) { /* 判断条件并选择处理分支。 */
			return model.ErrRawConflict /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		// A prior write may have renamed successfully but failed its directory
		// flush. Retrying the receipt must complete that flush before success.
		return syncDirectory(q.root) /* 返回当前处理结果。 */
	} else if !os.IsNotExist(err) { /* 结束当前表达式或代码块。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	entries, err := os.ReadDir(q.root) /* 更新 err 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	items := 0                      /* 更新 items 的值。 */
	for _, entry := range entries { /* 循环处理当前数据。 */
		if queueEntry(entry.Name()) { /* 判断条件并选择处理分支。 */
			items++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if items >= q.maxItems || q.used+int64(len(b)) > q.maxBytes { /* 判断条件并选择处理分支。 */
		return ErrQueueFull /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := atomicFile(target, b); err != nil { /* 判断条件并选择处理分支。 */
		// Keep capacity accounting correct after a post-rename flush failure.
		if info, statErr := os.Stat(target); statErr == nil { /* 判断条件并选择处理分支。 */
			q.used += info.Size() /* 更新 q.used 的值。 */
		} /* 结束当前表达式或代码块。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	q.used += int64(len(b)) /* 更新 q.used 的值。 */
	return nil              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (q *Queue) entries() ([]string, error) { /* 定义 entries 函数。 */
	all, err := os.ReadDir(q.root) /* 更新 err 的值。 */
	if err != nil {                /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := []string{}       /* 更新 out 的值。 */
	for _, e := range all { /* 循环处理当前数据。 */
		if strings.HasSuffix(e.Name(), ".json") { /* 判断条件并选择处理分支。 */
			out = append(out, e.Name()) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Strings(out) /* 执行当前语句并推进处理流程。 */
	return out, nil   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (q *Queue) Next() (model.RawMessage, bool, error) { /* 定义 Next 函数。 */
	q.mu.Lock()              /* 执行当前语句并推进处理流程。 */
	defer q.mu.Unlock()      /* 安排函数结束时执行清理。 */
	var raw model.RawMessage /* 声明 raw。 */
	if q.closed {            /* 判断条件并选择处理分支。 */
		return raw, false, os.ErrClosed /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	entries, err := q.entries()          /* 更新 err 的值。 */
	if err != nil || len(entries) == 0 { /* 判断条件并选择处理分支。 */
		return raw, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	path := filepath.Join(q.root, entries[0]) /* 更新 path 的值。 */
	info, err := os.Lstat(path)               /* 更新 err 的值。 */
	if err != nil {                           /* 判断条件并选择处理分支。 */
		return raw, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !info.Mode().IsRegular() { /* 判断条件并选择处理分支。 */
		return raw, false, errors.New("queue entry is not a regular file") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	f, err := os.Open(path) /* 更新 err 的值。 */
	if err != nil {         /* 判断条件并选择处理分支。 */
		return raw, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b, readErr := io.ReadAll(io.LimitReader(f, (1<<20)+1)) /* 更新 readErr 的值。 */
	closeErr := f.Close()                                  /* 更新 closeErr 的值。 */
	if readErr != nil {                                    /* 判断条件并选择处理分支。 */
		return raw, false, readErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if closeErr != nil { /* 判断条件并选择处理分支。 */
		return raw, false, closeErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	reason := ""        /* 更新 reason 的值。 */
	if len(b) > 1<<20 { /* 判断条件并选择处理分支。 */
		reason = "entry exceeds 1 MiB" /* 更新 reason 的值。 */
	} else if json.Unmarshal(b, &raw) != nil { /* 结束当前表达式或代码块。 */
		reason = "invalid JSON" /* 更新 reason 的值。 */
	} else if raw.TenantID == "" || raw.MessageID == "" || queueName(raw) != entries[0] { /* 结束当前表达式或代码块。 */
		reason = "identity checksum mismatch" /* 更新 reason 的值。 */
	} /* 结束当前表达式或代码块。 */
	if reason != "" { /* 判断条件并选择处理分支。 */
		// Retain the exact bytes in a separate quarantine. Operator retry of
		// server rejections must never requeue unreadable or mismatched data.
		if _, err := os.Lstat(path + ".corrupt"); err == nil { /* 判断条件并选择处理分支。 */
			return raw, false, errors.New("corrupt quarantine already exists") /* 返回当前处理结果。 */
		} else if !os.IsNotExist(err) { /* 结束当前表达式或代码块。 */
			return raw, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := os.Rename(path, path+".corrupt"); err != nil { /* 判断条件并选择处理分支。 */
			return raw, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := syncDirectory(q.root); err != nil { /* 判断条件并选择处理分支。 */
			return raw, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return model.RawMessage{}, false, fmt.Errorf("queue entry %s quarantined: %s", entries[0], reason) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return raw, true, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (q *Queue) Ack(raw model.RawMessage) error { /* 定义 Ack 函数。 */
	q.mu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer q.mu.Unlock() /* 安排函数结束时执行清理。 */
	if q.closed {       /* 判断条件并选择处理分支。 */
		return os.ErrClosed /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	path := filepath.Join(q.root, queueName(raw)) /* 更新 path 的值。 */
	info, err := os.Stat(path)                    /* 更新 err 的值。 */
	if os.IsNotExist(err) {                       /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = os.Remove(path); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	q.used -= info.Size()        /* 更新 q.used 的值。 */
	return syncDirectory(q.root) /* 返回当前处理结果。 */
}                           /* 结束当前表达式或代码块。 */
func (q *Queue) Depth() int { q.mu.Lock(); defer q.mu.Unlock(); v, _ := q.entries(); return len(v) } /* 定义 Depth 函数。 */
func atomicFile(path string, b []byte) error { /* 定义 atomicFile 函数。 */
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-") /* 更新 err 的值。 */
	if err != nil {                                          /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer os.Remove(f.Name())            /* 安排函数结束时执行清理。 */
	if err = f.Chmod(0600); err == nil { /* 判断条件并选择处理分支。 */
		_, err = f.Write(b) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = f.Sync() /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	closeErr := f.Close() /* 更新 closeErr 的值。 */
	if err != nil {       /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if closeErr != nil { /* 判断条件并选择处理分支。 */
		return closeErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = os.Rename(f.Name(), path); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return syncDirectory(filepath.Dir(path)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Reject retains permanent failures on disk while allowing later messages to upload.
func (q *Queue) Reject(raw model.RawMessage) error { /* 定义 Reject 函数。 */
	q.mu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer q.mu.Unlock() /* 安排函数结束时执行清理。 */
	if q.closed {       /* 判断条件并选择处理分支。 */
		return os.ErrClosed /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	path := filepath.Join(q.root, queueName(raw))             /* 更新 path 的值。 */
	if err := os.Rename(path, path+".rejected"); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return syncDirectory(q.root) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (q *Queue) Rejected() int { /* 定义 Rejected 函数。 */
	q.mu.Lock()                      /* 执行当前语句并推进处理流程。 */
	defer q.mu.Unlock()              /* 安排函数结束时执行清理。 */
	entries, _ := os.ReadDir(q.root) /* 更新 _ 的值。 */
	count := 0                       /* 更新 count 的值。 */
	for _, entry := range entries {  /* 循环处理当前数据。 */
		if strings.HasSuffix(entry.Name(), ".rejected") { /* 判断条件并选择处理分支。 */
			count++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return count /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// RetryRejected is an explicit operator action after correcting assignments or data.
func (q *Queue) RetryRejected() error { /* 定义 RetryRejected 函数。 */
	q.mu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer q.mu.Unlock() /* 安排函数结束时执行清理。 */
	if q.closed {       /* 判断条件并选择处理分支。 */
		return os.ErrClosed /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	entries, err := os.ReadDir(q.root) /* 更新 err 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, entry := range entries { /* 循环处理当前数据。 */
		if strings.HasSuffix(entry.Name(), ".json.rejected") { /* 判断条件并选择处理分支。 */
			source := filepath.Join(q.root, entry.Name())     /* 更新 source 的值。 */
			target := strings.TrimSuffix(source, ".rejected") /* 更新 target 的值。 */
			if _, err := os.Stat(target); err == nil {        /* 判断条件并选择处理分支。 */
				return errors.New("active queue already contains rejected id") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if err := os.Rename(source, target); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return syncDirectory(q.root) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func queueEntry(name string) bool { /* 定义 queueEntry 函数。 */
	return strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".rejected") || strings.HasSuffix(name, ".corrupt") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (q *Queue) Corrupt() int { /* 定义 Corrupt 函数。 */
	q.mu.Lock()                      /* 执行当前语句并推进处理流程。 */
	defer q.mu.Unlock()              /* 安排函数结束时执行清理。 */
	entries, _ := os.ReadDir(q.root) /* 更新 _ 的值。 */
	count := 0                       /* 更新 count 的值。 */
	for _, entry := range entries {  /* 循环处理当前数据。 */
		if strings.HasSuffix(entry.Name(), ".corrupt") { /* 判断条件并选择处理分支。 */
			count++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return count /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
