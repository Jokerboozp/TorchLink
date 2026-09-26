package model

import "strings"

func ProtocolPlatforms() []string {
	return []string{"linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64", "darwin-amd64", "darwin-arm64"}
}

// Accept the historical slash notation used by early Edge configurations.
func ProtocolPlatform(value string) string {
	value = strings.ReplaceAll(value, "/", "-")
	for _, candidate := range ProtocolPlatforms() {
		if value == candidate {
			return value
		}
	}
	return ""
}
