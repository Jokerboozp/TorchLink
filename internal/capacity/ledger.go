package capacity

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"iot-platform/internal/onboarding"
)

// LedgerHeader is the first line of every ledger file.
type LedgerHeader struct {
	RunID      string `json:"runId"`
	Agent      string `json:"agent"`
	Generation int64  `json:"generation"`
	PhaseID    string `json:"phaseId"`
	Tenant     string `json:"tenant"`
	Product    string `json:"product"`
}

// LedgerEntry is one logical message. Retries of the same message keep the
// same client ID and bytes and only raise Attempts.
type LedgerEntry struct {
	Seq        uint64 `json:"q"`
	Stream     string `json:"s"`
	Measured   bool   `json:"m"`
	Device     string `json:"d,omitempty"`
	ClientID   string `json:"c,omitempty"`
	RawID      string `json:"r,omitempty"`
	Hash       string `json:"h,omitempty"`
	Bytes      int    `json:"b,omitempty"`
	Scheduled  int64  `json:"t"`            // agent clock, Unix microseconds
	Dispatched int64  `json:"dt,omitempty"` // 0 when never sent
	Responded  int64  `json:"rt,omitempty"`
	Attempts   int    `json:"a,omitempty"`
	Result     string `json:"x"`
	OK         bool   `json:"ok,omitempty"`
	// Alarm is the stressAlarm flag the report carried (alarm sequence check).
	Alarm bool `json:"al,omitempty"`
}

// LedgerWriter appends gzip JSONL through one goroutine so request goroutines
// never block on disk for longer than a channel send.
type LedgerWriter struct {
	path    string
	f       *os.File
	gz      *gzip.Writer
	bw      *bufio.Writer
	ch      chan LedgerEntry
	done    chan struct{}
	err     error
	entries uint64
	once    sync.Once
}

func NewLedgerWriter(path string, h LedgerHeader) (*LedgerWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	gz := gzip.NewWriter(f)
	w := &LedgerWriter{path: path, f: f, gz: gz, bw: bufio.NewWriterSize(gz, 1<<16), ch: make(chan LedgerEntry, 8192), done: make(chan struct{})}
	b, _ := json.Marshal(h)
	_, _ = w.bw.Write(append(b, '\n'))
	go w.loop()
	return w, nil
}

func (w *LedgerWriter) loop() {
	defer close(w.done)
	enc := json.NewEncoder(w.bw)
	for e := range w.ch {
		if w.err == nil {
			w.err = enc.Encode(e)
			w.entries++
		}
	}
}

func (w *LedgerWriter) Write(e LedgerEntry) { w.ch <- e }

// Close flushes and returns the entry count and file digest.
func (w *LedgerWriter) Close() (LedgerInfo, error) {
	w.once.Do(func() { close(w.ch) })
	<-w.done
	err := errors.Join(w.err, w.bw.Flush(), w.gz.Close(), w.f.Close())
	info := LedgerInfo{Entries: w.entries}
	if err != nil {
		return info, err
	}
	info.SHA256, info.Bytes, err = fileDigest(w.path)
	return info, err
}

type LedgerInfo struct {
	Entries uint64 `json:"entries"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
}

func fileDigest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// ReadLedger streams a ledger file to fn.
func ReadLedger(path string, fn func(LedgerHeader, LedgerEntry) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var h LedgerHeader
	first := true
	for sc.Scan() {
		if first {
			first = false
			if err = json.Unmarshal(sc.Bytes(), &h); err != nil {
				return fmt.Errorf("ledger header: %w", err)
			}
			continue
		}
		var e LedgerEntry
		if err = json.Unmarshal(sc.Bytes(), &e); err != nil {
			return fmt.Errorf("ledger entry: %w", err)
		}
		if err = fn(h, e); err != nil {
			return err
		}
	}
	return sc.Err()
}

// StandardRawID mirrors onboarding.StandardRaw: the raw message ID is derived
// from identity, message kind and the client ID, so it is known even when the
// ingest response was lost.
func StandardRawID(tenant, product, device, kind, clientID string) string {
	return "raw_std_" + onboarding.Hash(tenant + "\x00" + product + "\x00" + device + "\x00" + kind + "\x00" + clientID)[:32]
}

func payloadHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
