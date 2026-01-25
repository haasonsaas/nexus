package packs

// Pack defines a knowledge pack metadata file.
type Pack struct {
	Name        string         `yaml:"name" json:"name"`
	Version     string         `yaml:"version" json:"version"`
	Description string         `yaml:"description" json:"description"`
	Documents   []PackDocument `yaml:"documents" json:"documents"`
}

// PackDocument describes a document within a pack.
type PackDocument struct {
	Name        string   `yaml:"name" json:"name"`
	Path        string   `yaml:"path" json:"path"`
	ContentType string   `yaml:"content_type" json:"content_type"`
	Tags        []string `yaml:"tags" json:"tags"`
	Source      string   `yaml:"source" json:"source"`
}
