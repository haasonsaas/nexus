package bundled

import (
	"embed"
	"io/fs"
)

//go:embed hooks/**/HOOK.md
var bundledFS embed.FS

// BundledFS returns the embedded filesystem containing bundled hooks.
func BundledFS() fs.FS {
	// Return the hooks subdirectory as the root
	sub, err := fs.Sub(bundledFS, "hooks")
	if err != nil {
		// This should never happen with a valid embed
		return bundledFS
	}
	return sub
}
