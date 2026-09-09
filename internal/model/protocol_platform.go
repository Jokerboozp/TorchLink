package model

import (
	"errors"
	"strings"
)

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

// SelectProtocolArtifact returns a copy; version metadata remains immutable.
func SelectProtocolArtifact(artifact map[string]any, platform string) (map[string]any, error) {
	platform = ProtocolPlatform(platform)
	native, _ := artifact["platform"].(string)
	if platform == "" {
		return nil, errors.New("unsupported worker platform")
	}
	out := map[string]any{}
	for key, value := range artifact {
		out[key] = value
	}
	if ProtocolPlatform(native) != platform {
		variants, _ := artifact["variants"].(map[string]any)
		variant, ok := variants[platform].(map[string]any)
		if !ok {
			return nil, errors.New("published worker has no artifact for this node platform")
		}
		for _, key := range []string{"path", "sha256", "size", "platform", "validation", "testCases"} {
			out[key] = variant[key]
		}
	}
	out["platform"] = platform
	return out, nil
}
