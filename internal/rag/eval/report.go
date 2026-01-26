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

	Answer         string   `json:"answer"`
	Relevance      float64  `json:"relevance"`
	Faithfulness   float64  `json:"faithfulness"`
	ContextRecall  float64  `json:"context_recall"`
	Judged         bool     `json:"judged"`
	AnswerExpected int      `json:"answer_expected"`
	AnswerMatched  int      `json:"answer_matched"`
	AnswerCoverage float64  `json:"answer_coverage"`
	AnswerMissing  []string `json:"answer_missing,omitempty"`

	ExpectedHints []ExpectedChunk `json:"expected_chunks"`
}

// Summary aggregates metrics across cases.
type Summary struct {
	AvgPrecision      float64 `json:"avg_precision"`
	AvgRecall         float64 `json:"avg_recall"`
	AvgMRR            float64 `json:"avg_mrr"`
	AvgNDCG           float64 `json:"avg_ndcg"`
	AvgRelevance      float64 `json:"avg_relevance"`
	AvgFaithfulness   float64 `json:"avg_faithfulness"`
	AvgContextRecall  float64 `json:"avg_context_recall"`
	AvgAnswerCoverage float64 `json:"avg_answer_coverage"`
	Cases             int     `json:"cases"`
	JudgeCases        int     `json:"judge_cases"`
	AnswerCases       int     `json:"answer_cases"`
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
		if c.Judged {
			s.AvgRelevance += c.Relevance
			s.AvgFaithfulness += c.Faithfulness
			s.AvgContextRecall += c.ContextRecall
			s.JudgeCases++
		}
		if c.AnswerExpected > 0 {
			s.AvgAnswerCoverage += c.AnswerCoverage
			s.AnswerCases++
		}
	}
	count := float64(len(cases))
	s.AvgPrecision /= count
	s.AvgRecall /= count
	s.AvgMRR /= count
	s.AvgNDCG /= count
	if s.JudgeCases > 0 {
		judgeCount := float64(s.JudgeCases)
		s.AvgRelevance /= judgeCount
		s.AvgFaithfulness /= judgeCount
		s.AvgContextRecall /= judgeCount
	}
	if s.AnswerCases > 0 {
		s.AvgAnswerCoverage /= float64(s.AnswerCases)
	}
	return s
}
