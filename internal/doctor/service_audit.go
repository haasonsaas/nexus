package doctor

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
)

// ServiceAudit captures service file hints and port checks.
type ServiceAudit struct {
	SystemdUser   []string
	SystemdSystem []string
	LaunchdUser   []string
	LaunchdSystem []string
	Ports         []PortStatus
}

// PortStatus reports port availability.
type PortStatus struct {
	Port  int
	InUse bool
	Error string
}

// AuditServices inspects common service file locations and port availability.
func AuditServices(cfg *config.Config) ServiceAudit {
	audit := ServiceAudit{}

	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}

	if runtime.GOOS != "windows" {
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".config")
		}
		audit.SystemdUser = findServiceFiles(filepath.Join(xdg, "systemd", "user"), ".service", "nexus")
		audit.SystemdSystem = findServiceFiles("/etc/systemd/system", ".service", "nexus")

		if runtime.GOOS == "darwin" {
			audit.LaunchdUser = findServiceFiles(filepath.Join(home, "Library", "LaunchAgents"), ".plist", "nexus")
			audit.LaunchdSystem = findServiceFiles("/Library/LaunchDaemons", ".plist", "nexus")
		}
	}

	if cfg != nil {
		host := normalizeHost(cfg.Server.Host)
		if cfg.Server.GRPCPort > 0 {
			audit.Ports = append(audit.Ports, CheckPort(host, cfg.Server.GRPCPort))
		}
		if cfg.Server.HTTPPort > 0 {
			audit.Ports = append(audit.Ports, CheckPort(host, cfg.Server.HTTPPort))
		}
	}

	return audit
}

// CheckPort attempts to listen to a port to detect collisions.
func CheckPort(host string, port int) PortStatus {
	status := PortStatus{Port: port}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		status.InUse = true
		status.Error = err.Error()
		return status
	}
	_ = listener.Close()
	return status
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}

func findServiceFiles(dir string, suffix string, contains string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var matches []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		if contains != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(contains)) {
			continue
		}
		matches = append(matches, filepath.Join(dir, name))
	}
	if len(matches) > 1 {
		sort.Strings(matches)
	}
	return matches
}
