package main

// NodePolicy defines local policy controls for node tools.
type NodePolicy struct {
	Shell *ShellPolicy `json:"shell,omitempty" yaml:"shell,omitempty"`
}

// ShellPolicy controls command execution for nodes.shell_run.
type ShellPolicy struct {
	Allowlist []string `json:"allowlist,omitempty" yaml:"allowlist,omitempty"`
	Denylist  []string `json:"denylist,omitempty" yaml:"denylist,omitempty"`
}
