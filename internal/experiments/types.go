package experiments

// Config defines experiment configuration.
type Config struct {
	Experiments []Experiment `yaml:"experiments"`
}

// Experiment defines a single experiment.
type Experiment struct {
	ID          string    `yaml:"id"`
	Description string    `yaml:"description"`
	Status      string    `yaml:"status"`     // active | inactive
	Allocation  int       `yaml:"allocation"` // percentage 0-100
	Variants    []Variant `yaml:"variants"`
}

// Variant defines a variant within an experiment.
type Variant struct {
	ID     string        `yaml:"id"`
	Weight int           `yaml:"weight"`
	Config VariantConfig `yaml:"config"`
}

// VariantConfig defines per-variant overrides.
type VariantConfig struct {
	SystemPrompt string `yaml:"system_prompt"`
	Provider     string `yaml:"provider"`
	Model        string `yaml:"model"`
}

// Assignment records a subject's experiment variant.
type Assignment struct {
	ExperimentID string `json:"experiment_id"`
	VariantID    string `json:"variant_id"`
}

// Overrides represents merged experiment overrides.
type Overrides struct {
	SystemPrompt string
	Provider     string
	Model        string
	Assignments  []Assignment
}
