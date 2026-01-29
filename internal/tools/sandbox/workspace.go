package sandbox

import "strings"

// ParseWorkspaceAccess converts a config string to a workspace access mode.
func ParseWorkspaceAccess(raw string) WorkspaceAccessMode {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "rw", "readwrite", "read-write", "write":
		return WorkspaceReadWrite
	case "none", "disabled":
		return WorkspaceNone
	case "ro", "readonly", "read-only":
		return WorkspaceReadOnly
	default:
		return WorkspaceReadOnly
	}
}
