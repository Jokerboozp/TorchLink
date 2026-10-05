package capacity

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// Inventory is the operator-registered target. The controller only contacts
// addresses listed here; a plan cannot introduce arbitrary hosts.
type Inventory struct {
	Name string `yaml:"name" json:"name"`
	API  string `yaml:"api" json:"api"`
	MQTT string `yaml:"mqtt,omitempty" json:"mqtt,omitempty"`
	TCP  string `yaml:"tcp,omitempty" json:"tcp,omitempty"`
	// Web is the management web origin (HLS playback goes through its proxy).
	Web     string          `yaml:"web,omitempty" json:"web,omitempty"`
	Metrics []MetricsTarget `yaml:"metrics" json:"metrics"`
	// Nodes are node-exporter endpoints of the hosts under test; they are
	// sampled separately into observations/nodes.jsonl for host charts.
	Nodes     []NodeTarget  `yaml:"nodes,omitempty" json:"nodes,omitempty"`
	Agents    []AgentTarget `yaml:"agents" json:"agents"`
	Observers Observers     `yaml:"observers" json:"observers"`
}

type MetricsTarget struct {
	Role     string `yaml:"role" json:"role"`
	Instance string `yaml:"instance" json:"instance"`
	URL      string `yaml:"url" json:"url"`
	// Token is the platform's IOT_METRICS_TOKEN; it never leaves the process.
	Token string `yaml:"-" json:"-"`
}

type NodeTarget struct {
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url" json:"url"`
}

// AgentTarget with an empty URL runs in the controller process.
type AgentTarget struct {
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url,omitempty" json:"url,omitempty"`
}

// Observers name read-only data-store secrets used for reconciliation.
type Observers struct {
	PostgresSecretRef   string `yaml:"postgresSecretRef" json:"postgresSecretRef"`
	ClickHouseSecretRef string `yaml:"clickhouseSecretRef,omitempty" json:"clickhouseSecretRef,omitempty"`
}

func LoadInventory(path string) (*Inventory, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var inv Inventory
	if err = dec.Decode(&inv); err != nil {
		return nil, fmt.Errorf("inventory: %w", err)
	}
	return &inv, inv.Validate()
}

func (inv *Inventory) Validate() error {
	var errs []string
	bad := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	if !identifier.MatchString(inv.Name) {
		bad("inventory name must be a plain identifier")
	}
	if !httpURL(inv.API) {
		bad("inventory api must be an http(s) URL")
	}
	if inv.MQTT != "" {
		if u, err := url.Parse(inv.MQTT); err != nil || (u.Scheme != "tcp" && u.Scheme != "ssl" && u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
			bad("inventory mqtt must be tcp://, ssl://, ws:// or wss://")
		}
	}
	seenNode := map[string]bool{}
	for i, n := range inv.Nodes {
		if !identifier.MatchString(n.Name) || seenNode[n.Name] || !httpURL(n.URL) {
			bad("nodes[%d] needs a unique name and an http(s) node-exporter URL", i)
		}
		seenNode[n.Name] = true
	}
	if inv.TCP != "" {
		if _, _, err := net.SplitHostPort(inv.TCP); err != nil {
			bad("inventory tcp must be host:port")
		}
	}
	if len(inv.Metrics) == 0 {
		bad("inventory metrics must list every platform instance; backlog cannot be judged without them")
	}
	seen := map[string]bool{}
	for _, m := range inv.Metrics {
		if !identifier.MatchString(m.Instance) || !identifier.MatchString(m.Role) || !httpURL(m.URL) || seen[m.Instance] {
			bad("metrics target %q needs a unique instance, a role and an http(s) URL", m.Instance)
		}
		seen[m.Instance] = true
	}
	if len(inv.Agents) == 0 {
		bad("inventory agents must list at least one agent (an entry without url runs in-process)")
	}
	names := map[string]bool{}
	for _, a := range inv.Agents {
		if !identifier.MatchString(a.Name) || names[a.Name] || (a.URL != "" && !httpURL(a.URL)) {
			bad("agent %q needs a unique name and, when remote, an http(s) URL", a.Name)
		}
		names[a.Name] = true
	}
	if inv.Observers.PostgresSecretRef == "" {
		bad("inventory observers.postgresSecretRef is required for ID reconciliation")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Secrets resolves secret references from TORCHLINK_CAPACITY_SECRET_<NAME>
// environment variables or a private YAML file of name: value pairs.
type Secrets struct {
	file map[string]string
}

func LoadSecrets(path string) (*Secrets, error) {
	s := &Secrets{file: map[string]string{}}
	if path == "" {
		return s, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("secrets file %s must not be readable by group or others (chmod 600)", filepath.Base(path))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err = yaml.Unmarshal(b, &s.file); err != nil {
		return nil, errors.New("secrets file is not a YAML map of name: value")
	}
	return s, nil
}

func SecretEnvName(ref string) string {
	return "TORCHLINK_CAPACITY_SECRET_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(ref))
}

func (s *Secrets) Get(ref string) (string, error) {
	if ref == "" {
		return "", errors.New("empty secret reference")
	}
	if v := strings.TrimSpace(os.Getenv(SecretEnvName(ref))); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(s.file[ref]); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("secret %q is not set (use %s or the secrets file)", ref, SecretEnvName(ref))
}

// Set adds a named secret held only in memory (for example the observer DSN
// of the capacity module, taken from its environment).
func (s *Secrets) Set(ref, value string) {
	if s.file == nil {
		s.file = map[string]string{}
	}
	s.file[ref] = value
}

// AllValues lists every secret from the file and TORCHLINK_CAPACITY_SECRET_*
// variables, for leak checks when the plan is not at hand.
func (s *Secrets) AllValues() []string {
	var out []string
	for _, v := range s.file {
		if v = strings.TrimSpace(v); len(v) >= 4 {
			out = append(out, v)
		}
	}
	for _, kv := range os.Environ() {
		if name, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(name, "TORCHLINK_CAPACITY_SECRET_") && len(strings.TrimSpace(v)) >= 4 {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

// Values lists every resolved secret so evidence writers can assert none leaked.
func (s *Secrets) Values(refs ...string) []string {
	var out []string
	for _, r := range refs {
		if v, err := s.Get(r); err == nil && len(v) >= 4 {
			out = append(out, v)
		}
	}
	return out
}
