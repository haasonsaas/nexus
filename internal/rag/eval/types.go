package eval

// TestSet defines a RAG evaluation dataset.
type TestSet struct {
	Version int        `yaml:"version" json:"version"`
	Name    string     `yaml:"name" json:"name"`
	Cases   []TestCase `yaml:"cases" json:"cases"`
}

// TestCase defines a single evaluation query and expectations.
type TestCase struct {
	ID                     string          `yaml:"id" json:"id"`
	Query                  string          `yaml:"query" json:"query"`
	ExpectedChunks         []ExpectedChunk `yaml:"expected_chunks" json:"expected_chunks"`
	ExpectedAnswerContains []string        `yaml:"expected_answer_contains" json:"expected_answer_contains"`
}

// ExpectedChunk describes a relevant chunk for retrieval evaluation.
type ExpectedChunk struct {
	DocID   string `yaml:"doc_id" json:"doc_id"`
	Section string `yaml:"section" json:"section"`
}

// ResultKey identifies a retrieved chunk for metric comparison.
type ResultKey struct {
	DocID   string
	Section string
}
