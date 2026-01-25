package eval

import "time"

// Report captures evaluation results and aggregated metrics.
type Report struct {
	GeneratedAt time.Time    `json:"generated_at"`
	TestSetName string       `json:"test_set_name"`
	Summary     Summary      `json:"summary"`
	Cases       []CaseResult `json:"cases"`
}

// CaseResult contains metrics for a single test case.
type CaseResult struct {
	CaseID    string        `json:"case_id"`
	Query     string        `json:"query"`
	Retrieved int           `json:"retrieved"`
	Expected  int           `json:"expected"`
	Precision float64       `json:"precision"`
	Recall    float64       `json:"recall"`
	MRR       float64       `json:"mrr"`
	NDCG      float64       `json:"ndcg"`
	QueryTime time.Duration `json:"query_time"`

	ExpectedHints []ExpectedChunk `json:"expected_chunks"`
}

// Summary aggregates metrics across cases.
type Summary struct {
	AvgPrecision float64 `json:"avg_precision"`
	AvgRecall    float64 `json:"avg_recall"`
	AvgMRR       float64 `json:"avg_mrr"`
	AvgNDCG      float64 `json:"avg_ndcg"`
	Cases        int     `json:"cases"`
}

func summarize(cases []CaseResult) Summary {
	if len(cases) == 0 {
		return Summary{}
	}
	s := Summary{Cases: len(cases)}
	for _, c := range cases {
		s.AvgPrecision += c.Precision
		s.AvgRecall += c.Recall
		s.AvgMRR += c.MRR
		s.AvgNDCG += c.NDCG
	}
	count := float64(len(cases))
	s.AvgPrecision /= count
	s.AvgRecall /= count
	s.AvgMRR /= count
	s.AvgNDCG /= count
	return s
}
