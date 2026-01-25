package experiments

import (
	"hash/fnv"
	"strings"
)

// Manager evaluates experiments and assigns variants.
type Manager struct {
	experiments []Experiment
}

// NewManager creates a new experiments manager.
func NewManager(cfg Config) *Manager {
	active := make([]Experiment, 0, len(cfg.Experiments))
	for _, exp := range cfg.Experiments {
		if strings.EqualFold(strings.TrimSpace(exp.Status), "active") {
			active = append(active, exp)
		}
	}
	return &Manager{experiments: active}
}

// Resolve returns merged overrides for the subject.
func (m *Manager) Resolve(subject string) Overrides {
	var out Overrides
	if subject == "" || len(m.experiments) == 0 {
		return out
	}
	for _, exp := range m.experiments {
		variant := selectVariant(subject, exp)
		if variant == nil {
			continue
		}
		out.Assignments = append(out.Assignments, Assignment{
			ExperimentID: exp.ID,
			VariantID:    variant.ID,
		})
		if variant.Config.SystemPrompt != "" {
			out.SystemPrompt = variant.Config.SystemPrompt
		}
		if variant.Config.Provider != "" {
			out.Provider = variant.Config.Provider
		}
		if variant.Config.Model != "" {
			out.Model = variant.Config.Model
		}
	}
	return out
}

func selectVariant(subject string, exp Experiment) *Variant {
	if exp.ID == "" || exp.Allocation <= 0 || len(exp.Variants) == 0 {
		return nil
	}
	bucket := int(hashUint32(subject+":"+exp.ID) % 100)
	if bucket >= exp.Allocation {
		return nil
	}
	totalWeight := 0
	for _, v := range exp.Variants {
		if v.Weight > 0 {
			totalWeight += v.Weight
		}
	}
	if totalWeight == 0 {
		return nil
	}
	pick := int(hashUint32(subject+":"+exp.ID+":variant") % uint32(totalWeight))
	for i := range exp.Variants {
		v := exp.Variants[i]
		if v.Weight <= 0 {
			continue
		}
		if pick < v.Weight {
			return &exp.Variants[i]
		}
		pick -= v.Weight
	}
	return nil
}

func hashUint32(value string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return h.Sum32()
}
