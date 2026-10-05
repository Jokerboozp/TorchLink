// Package version reports which build is running. Release images set Version
// and Revision with -ldflags -X; source runs report "dev" and, inside a Git
// checkout, the commit Go recorded at build time.
package version

import "runtime/debug"

var (
	Version  = "dev"
	Revision = ""
)

// Commit returns the injected revision or the one Go embedded from Git.
func Commit() string {
	if Revision != "" {
		return Revision
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return ""
}
