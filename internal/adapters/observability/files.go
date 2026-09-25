package observability

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/ports"
)

const historyKeep = 20

var managedFileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,80}$`)

// DirStore keeps one file per rule group in a directory shared with a
// component (Prometheus rule_files glob, Loki local ruler directory).
// Disabled groups and previous versions live in a platform-private state
// directory so the component never loads them.
type DirStore struct {
	active   string
	disabled string
	history  string
	ext      string
	mode     os.FileMode
	mu       sync.Mutex
}

func NewDirStore(activeDir, stateDir, ext string, mode os.FileMode) *DirStore {
	if activeDir == "" {
		return &DirStore{}
	}
	return &DirStore{active: activeDir, disabled: filepath.Join(stateDir, "disabled"), history: filepath.Join(stateDir, "history"), ext: ext, mode: mode}
}

func (s *DirStore) Configured() bool { return s != nil && s.active != "" }

func revision(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:12])
}

func (s *DirStore) List() ([]ports.ManagedFile, error) {
	if !s.Configured() {
		return nil, ports.ErrOpsNotConfigured
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []ports.ManagedFile{}
	for _, dir := range []struct {
		path    string
		enabled bool
	}{{s.active, true}, {s.disabled, false}} {
		entries, err := os.ReadDir(dir.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), s.ext)
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), s.ext) || !managedFileName.MatchString(name) {
				continue
			}
			file, err := readManaged(filepath.Join(dir.path, entry.Name()), name, dir.enabled)
			if err != nil {
				return nil, err
			}
			out = append(out, file)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func readManaged(path, name string, enabled bool) (ports.ManagedFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ports.ManagedFile{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return ports.ManagedFile{}, err
	}
	return ports.ManagedFile{Name: name, Enabled: enabled, Content: content, Revision: revision(content), ModTime: info.ModTime()}, nil
}

func (s *DirStore) Read(name string) (ports.ManagedFile, error) {
	if !s.Configured() {
		return ports.ManagedFile{}, ports.ErrOpsNotConfigured
	}
	if !managedFileName.MatchString(name) {
		return ports.ManagedFile{}, ports.ErrOpsNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, dir := range []struct {
		path    string
		enabled bool
	}{{s.active, true}, {s.disabled, false}} {
		file, err := readManaged(filepath.Join(dir.path, name+s.ext), name, dir.enabled)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return ports.ManagedFile{}, err
		}
	}
	return ports.ManagedFile{}, ports.ErrOpsNotFound
}

// Write atomically replaces the file in the enabled or disabled location and
// removes any copy in the other location. The previous content is archived.
func (s *DirStore) Write(name string, content []byte, enabled bool) error {
	if !s.Configured() {
		return ports.ErrOpsNotConfigured
	}
	if !managedFileName.MatchString(name) {
		return fmt.Errorf("invalid managed file name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	target, other := filepath.Join(s.active, name+s.ext), filepath.Join(s.disabled, name+s.ext)
	if !enabled {
		target, other = other, target
	}
	s.archive(name, target)
	s.archive(name, other)
	if err := atomicWrite(target, content, s.mode); err != nil {
		return err
	}
	if err := os.Remove(other); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *DirStore) Remove(name string) error {
	if !s.Configured() {
		return ports.ErrOpsNotConfigured
	}
	if !managedFileName.MatchString(name) {
		return ports.ErrOpsNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := false
	for _, path := range []string{filepath.Join(s.active, name+s.ext), filepath.Join(s.disabled, name+s.ext)} {
		s.archive(name, path)
		err := os.Remove(path)
		if err == nil {
			removed = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if !removed {
		return ports.ErrOpsNotFound
	}
	return nil
}

func (s *DirStore) archive(name, path string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}
	archiveVersion(filepath.Join(s.history, name), s.ext, content)
}

// archiveVersion keeps the most recent versions of a managed file for manual
// recovery. Archive failures never block the actual change.
func archiveVersion(dir, ext string, content []byte) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, time.Now().UTC().Format("20060102T150405.000000000Z")+ext), content, 0o600)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= historyKeep {
		return
	}
	for _, entry := range entries[:len(entries)-historyKeep] {
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// FileStore manages a single shared configuration file.
type FileStore struct {
	path    string
	history string
	mode    os.FileMode
	mu      sync.Mutex
}

func NewFileStore(path, stateDir string, mode os.FileMode) *FileStore {
	if path == "" {
		return &FileStore{}
	}
	return &FileStore{path: path, history: filepath.Join(stateDir, "history", strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))), mode: mode}
}

func (f *FileStore) Configured() bool { return f != nil && f.path != "" }

func (f *FileStore) Read() (ports.ManagedFile, error) {
	if !f.Configured() {
		return ports.ManagedFile{}, ports.ErrOpsNotConfigured
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	file, err := readManaged(f.path, filepath.Base(f.path), true)
	if errors.Is(err, os.ErrNotExist) {
		return ports.ManagedFile{Name: filepath.Base(f.path), Enabled: true, Revision: revision(nil)}, nil
	}
	return file, err
}

func (f *FileStore) Write(content []byte) error {
	if !f.Configured() {
		return ports.ErrOpsNotConfigured
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if previous, err := os.ReadFile(f.path); err == nil {
		archiveVersion(f.history, filepath.Ext(f.path), previous)
	}
	return atomicWrite(f.path, content, f.mode)
}

// atomicWrite writes to a temporary file in the target directory, syncs it
// and renames it over the target so readers never observe partial content.
func atomicWrite(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
