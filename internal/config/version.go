package config

import "fmt"

// CurrentVersion is the latest supported configuration file version.
const CurrentVersion = 1

// VersionError describes a configuration version mismatch.
type VersionError struct {
	Version int
	Current int
	Reason  string
}

func (e *VersionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason == "newer than this build" {
		return fmt.Sprintf("config version %d is newer than this build (current: %d). upgrade Nexus to continue", e.Version, e.Current)
	}
	if e.Reason != "" {
		return fmt.Sprintf("config version %d is %s (current: %d). run `nexus doctor --repair`", e.Version, e.Reason, e.Current)
	}
	return fmt.Sprintf("config version %d is unsupported (current: %d). run `nexus doctor --repair`", e.Version, e.Current)
}

// ValidateVersion ensures the provided config version is supported.
func ValidateVersion(version int) error {
	if version <= 0 {
		return &VersionError{Version: version, Current: CurrentVersion, Reason: "missing or outdated"}
	}
	if version < CurrentVersion {
		return &VersionError{Version: version, Current: CurrentVersion, Reason: "outdated"}
	}
	if version > CurrentVersion {
		return &VersionError{Version: version, Current: CurrentVersion, Reason: "newer than this build"}
	}
	return nil
}
