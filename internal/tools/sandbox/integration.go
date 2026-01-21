package sandbox

import (
	"github.com/haasonsaas/nexus/internal/agent"
)

// Register registers the sandbox executor as a tool with the agent runtime.
// This is a convenience function for integration with the Nexus agent.
func Register(runtime *agent.Runtime, opts ...Option) error {
	executor, err := NewExecutor(opts...)
	if err != nil {
		return err
	}

	runtime.RegisterTool(executor)
	return nil
}

// MustRegister registers the sandbox executor and panics on error.
// Use this in initialization code where errors should be fatal.
func MustRegister(runtime *agent.Runtime, opts ...Option) {
	if err := Register(runtime, opts...); err != nil {
		panic(err)
	}
}
