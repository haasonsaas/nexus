package eval

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadTestSet reads a YAML test set from disk.
func LoadTestSet(path string) (*TestSet, error) {
	if path == "" {
		return nil, fmt.Errorf("test set path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read test set: %w", err)
	}
	var set TestSet
	if err := yaml.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("parse test set: %w", err)
	}
	if len(set.Cases) == 0 {
		return nil, fmt.Errorf("test set has no cases")
	}
	for i, tc := range set.Cases {
		if tc.ID == "" {
			return nil, fmt.Errorf("test case %d missing id", i)
		}
		if tc.Query == "" {
			return nil, fmt.Errorf("test case %q missing query", tc.ID)
		}
	}
	return &set, nil
}
