package canvas

import "strings"

const (
	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleAdmin  = "admin"
)

func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleViewer:
		return RoleViewer
	case RoleAdmin:
		return RoleAdmin
	case RoleEditor:
		fallthrough
	default:
		return RoleEditor
	}
}

func RoleAllowsAction(role string) bool {
	normalized := NormalizeRole(role)
	return normalized == RoleEditor || normalized == RoleAdmin
}
