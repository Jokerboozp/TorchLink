package protocolcatalog

import (
	"strings"
	"testing"
	"time"
)

func TestProgramPlatformsAndSourceKinds(t *testing.T) {
	now := time.Now()
	entry := Entry{Kind: "edge-agent", Platform: "linux/arm64", ID: "iot-edge-agent", Version: "v2", Name: "Edge", SourceURL: "https://catalog.example.com/agent", Size: 1, SHA256: strings.Repeat("a", 64)}
	payload := Payload{IssuedAt: now.UnixMilli(), ExpiresAt: now.Add(time.Hour).UnixMilli(), Entries: []Entry{entry}}
	if err := Validate(payload, now); err != nil {
		t.Fatal(err)
	}
	other := entry
	other.Platform = "windows/amd64"
	payload.Entries = append(payload.Entries, other)
	if err := Validate(payload, now); err != nil {
		t.Fatal("multiple native platforms rejected", err)
	}
	for _, invalid := range []Entry{{Kind: "edge-agent", ID: "foreign", Platform: "linux/arm64"}, {Kind: "edge-agent", ID: "iot-edge-agent", Platform: "linux/unknown"}, {Kind: "unknown"}, {Kind: "protocol-source", Platform: "linux/arm64"}} {
		item := entry
		item.Kind, item.ID, item.Platform = invalid.Kind, invalid.ID, invalid.Platform
		payload.Entries = []Entry{item}
		if Validate(payload, now) == nil {
			t.Fatal("invalid program accepted", invalid)
		}
	}
	source := entry
	source.Kind, source.Platform = "", ""
	alias := source
	alias.Kind = "protocol-source"
	payload.Entries = []Entry{source, alias}
	if Validate(payload, now) == nil {
		t.Fatal("implicit and explicit source aliases duplicate an immutable version")
	}
}
