package main

// NodePolicy defines local policy controls for node tools.
type NodePolicy struct {
	Shell       *ShellPolicy       `json:"shell,omitempty" yaml:"shell,omitempty"`
	ComputerUse *ComputerUsePolicy `json:"computer_use,omitempty" yaml:"computer_use,omitempty"`
}

// ShellPolicy controls command execution for nodes.shell_run.
type ShellPolicy struct {
	Allowlist []string `json:"allowlist,omitempty" yaml:"allowlist,omitempty"`
	Denylist  []string `json:"denylist,omitempty" yaml:"denylist,omitempty"`
}

// ComputerUsePolicy controls action allow/deny lists for nodes.computer_use.
type ComputerUsePolicy struct {
	Allowlist []string `json:"allowlist,omitempty" yaml:"allowlist,omitempty"`
	Denylist  []string `json:"denylist,omitempty" yaml:"denylist,omitempty"`
}
