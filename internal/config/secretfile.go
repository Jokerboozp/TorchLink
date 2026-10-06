package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

// secretSuffixes name the variables that may be read from a file: for such a
// variable NAME, NAME_FILE holds the path of a file whose content is the
// value, so the secret need not appear in the process environment (Compose
// secrets mount it under /run/secrets).
var secretSuffixes = []string{"_SECRET", "_PASSWORD", "_TOKEN", "_KEY", "_DSN"}

const maxSecretFileBytes = 64 << 10

// ApplySecretFiles sets every secret variable NAME that is empty from its
// NAME_FILE. A trailing line break in the file is dropped. Setting both NAME
// and NAME_FILE is an error, as is an unreadable or oversized file.
func ApplySecretFiles() error {
	var errs []error
	for _, entry := range os.Environ() {
		key, path, _ := strings.Cut(entry, "=")
		name, ok := strings.CutSuffix(key, "_FILE")
		if !ok || !isSecretName(name) || strings.TrimSpace(path) == "" {
			continue
		}
		if os.Getenv(name) != "" {
			errs = append(errs, fmt.Errorf("set either %s or %s, not both", name, key))
			continue
		}
		value, err := readSecretFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
			continue
		}
		if err = os.Setenv(name, value); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}

func isSecretName(name string) bool {
	for _, suffix := range secretSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func readSecretFile(path string) (string, error) {
	f, err := os.Open(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxSecretFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxSecretFileBytes {
		return "", fmt.Errorf("file is larger than %d bytes", maxSecretFileBytes)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// withPassword puts password into a postgres:// DSN; an empty password or a
// DSN in key=value form is returned unchanged.
func withPassword(dsn, password string) string {
	if password == "" || dsn == "" {
		return dsn
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" || u.User == nil {
		return dsn
	}
	u.User = url.UserPassword(u.User.Username(), password)
	return u.String()
}
