package auth

import (
	"strings"
	"testing"

	"iot-platform/internal/ports"
)

// Business runs name tools through ports.MCPToolScope; it must produce the
// same scopes the MCP endpoint checks.
func TestPortsToolScopesMatchHarnessScopes(t *testing.T) {
	for _, scope := range HarnessReadScopes() {
		tool := strings.TrimPrefix(scope, "mcp:tool:")
		if ports.MCPToolScope(tool) != scope {
			t.Fatalf("ports.MCPToolScope(%q) = %q, want %q", tool, ports.MCPToolScope(tool), scope)
		}
	}
}
