package agent

import (
	"bytes"
	"encoding/json"
)

// hasManagedMcpConfig reports whether task MCP configuration was supplied.
// Empty objects add no servers and do not disable runtime inheritance.
func hasManagedMcpConfig(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}
