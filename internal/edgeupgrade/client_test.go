package edgeupgrade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalAndCacheBoundaries(t *testing.T) {
	root := t.TempDir()
	env := filepath.Join(root, "node.env")
	if err := os.WriteFile(env, []byte("# local fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	options := Options{URL: "https://platform.example.com", TenantID: "t", NodeID: "n", Secret: "fixture", DataDir: filepath.Join(root, "programs"), BootstrapBinary: filepath.Join(root, "bootstrap"), AgentEnvFile: env}
	l, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(l.options.DataDir, "agent-"+strings.Repeat("a", 64))
	discard := filepath.Join(l.options.DataDir, "agent-"+strings.Repeat("b", 64))
	unknown := filepath.Join(l.options.DataDir, "user-notes.txt")
	for _, path := range []string{keep, discard, unknown} {
		if err := os.WriteFile(path, []byte("local"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	l.state.CurrentPath = keep
	if err = l.prune(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(discard); !os.IsNotExist(err) {
		t.Fatal("unused owned cache retained", err)
	}
	for _, path := range []string{keep, unknown} {
		if _, err = os.Stat(path); err != nil {
			t.Fatal("unrelated/current file removed", err)
		}
	}
	l.Close()
	data, _ := json.Marshal(journal{CurrentPath: filepath.Join(root, "outside")})
	if err = os.WriteFile(filepath.Join(options.DataDir, "program.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if wrong, err := New(options); err == nil {
		wrong.Close()
		t.Fatal("journal escaped local program directory")
	}
	options.URL = "http://platform.example.com"
	if wrong, err := New(options); err == nil {
		wrong.Close()
		t.Fatal("implicit plaintext accepted")
	}
}
