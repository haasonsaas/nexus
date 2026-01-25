package eval

import "math"

// PrecisionRecall computes precision and recall for retrieved results.
func PrecisionRecall(retrieved []ResultKey, expected []ExpectedChunk) (precision, recall float64) {
	if len(retrieved) == 0 {
		return 0, 0
	}
	expectedSet := expectedKeySet(expected)
	relevant := 0
	for _, r := range retrieved {
		if expectedSet.matches(r) {
			relevant++
		}
	}
	precision = float64(relevant) / float64(len(retrieved))
	if len(expectedSet.keys) == 0 {
		return precision, 0
	}
	recall = float64(relevant) / float64(len(expectedSet.keys))
	return precision, recall
}

// MRR computes mean reciprocal rank for retrieved results.
func MRR(retrieved []ResultKey, expected []ExpectedChunk) float64 {
	if len(retrieved) == 0 {
		return 0
	}
	expectedSet := expectedKeySet(expected)
	for idx, r := range retrieved {
		if expectedSet.matches(r) {
			return 1.0 / float64(idx+1)
		}
	}
	return 0
}

// NDCG computes normalized discounted cumulative gain for binary relevance.
func NDCG(retrieved []ResultKey, expected []ExpectedChunk) float64 {
	if len(retrieved) == 0 {
		return 0
	}
	expectedSet := expectedKeySet(expected)
	if len(expectedSet.keys) == 0 {
		return 0
	}
	dcg := 0.0
	for idx, r := range retrieved {
		if expectedSet.matches(r) {
			dcg += 1.0 / math.Log2(float64(idx+2))
		}
	}
	idcg := idealDCG(len(expectedSet.keys), len(retrieved))
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func idealDCG(expectedCount, retrievedCount int) float64 {
	n := expectedCount
	if retrievedCount < n {
		n = retrievedCount
	}
	if n <= 0 {
		return 0
	}
	idcg := 0.0
	for i := 0; i < n; i++ {
		idcg += 1.0 / math.Log2(float64(i+2))
	}
	return idcg
}

type expectedSet struct {
	keys    map[ResultKey]struct{}
	docOnly map[string]struct{}
}

func expectedKeySet(expected []ExpectedChunk) expectedSet {
	set := expectedSet{
		keys:    make(map[ResultKey]struct{}),
		docOnly: make(map[string]struct{}),
	}
	for _, exp := range expected {
		key := ResultKey(exp)
		if exp.Section != "" {
			set.keys[key] = struct{}{}
			continue
		}
		if exp.DocID != "" {
			set.docOnly[exp.DocID] = struct{}{}
			set.keys[ResultKey{DocID: exp.DocID}] = struct{}{}
		}
	}
	return set
}

func (s expectedSet) matches(r ResultKey) bool {
	if _, ok := s.keys[r]; ok {
		return true
	}
	if r.DocID == "" {
		return false
	}
	_, ok := s.docOnly[r.DocID]
	return ok
}
