package agent

// ComputerUseConfig describes display configuration for computer use tools.
type ComputerUseConfig struct {
	DisplayWidthPx  int
	DisplayHeightPx int
	DisplayNumber   int
}

// ComputerUseConfigProvider is an optional interface for tools that expose computer-use display config.
type ComputerUseConfigProvider interface {
	ComputerUseConfig() *ComputerUseConfig
}
