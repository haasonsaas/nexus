package eval

import (
	"context"
	"fmt"
	"time"

	"github.com/haasonsaas/nexus/internal/rag/index"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Options controls evaluation behavior.
type Options struct {
	Limit     int
	Threshold float32
	Judge     bool
	Model     string
	MaxTokens int
}

// Evaluator runs RAG evaluation against a test set.
type Evaluator struct {
	index   *index.Manager
	options Options
	judge   *LLMJudge
}

// NewEvaluator creates a new evaluator.
func NewEvaluator(idx *index.Manager, opts *Options) *Evaluator {
	resolved := Options{Limit: 10, Threshold: 0.7}
	if opts != nil {
		if opts.Limit > 0 {
			resolved.Limit = opts.Limit
		}
		if opts.Threshold > 0 {
			resolved.Threshold = opts.Threshold
		}
		resolved.Judge = opts.Judge
		resolved.Model = opts.Model
		resolved.MaxTokens = opts.MaxTokens
	}
	return &Evaluator{index: idx, options: resolved}
}

// WithJudge attaches an LLM judge for answer quality scoring.
func (e *Evaluator) WithJudge(judge *LLMJudge) *Evaluator {
	e.judge = judge
	return e
}

// Evaluate runs the evaluation and returns a report.
func (e *Evaluator) Evaluate(ctx context.Context, set *TestSet) (*Report, error) {
	if set == nil {
		return nil, fmt.Errorf("test set is nil")
	}
	if e.index == nil {
		return nil, fmt.Errorf("index manager is nil")
	}
	results := make([]CaseResult, 0, len(set.Cases))
	for _, tc := range set.Cases {
		caseResult, err := e.evaluateCase(ctx, tc)
		if err != nil {
			return nil, err
		}
		results = append(results, caseResult)
	}
	report := &Report{
		GeneratedAt: time.Now(),
		TestSetName: set.Name,
		Cases:       results,
	}
	report.Summary = summarize(results)
	return report, nil
}

func (e *Evaluator) evaluateCase(ctx context.Context, tc TestCase) (CaseResult, error) {
	req := &models.DocumentSearchRequest{
		Query:           tc.Query,
		Limit:           e.options.Limit,
		Threshold:       e.options.Threshold,
		IncludeMetadata: true,
	}
	resp, err := e.index.Search(ctx, req)
	if err != nil {
		return CaseResult{}, fmt.Errorf("search failed for %s: %w", tc.ID, err)
	}

	retrievedKeys := make([]ResultKey, 0, len(resp.Results))
	for _, result := range resp.Results {
		if result == nil || result.Chunk == nil {
			continue
		}
		retrievedKeys = append(retrievedKeys, ResultKey{
			DocID:   result.Chunk.DocumentID,
			Section: result.Chunk.Metadata.Section,
		})
	}

	precision, recall := PrecisionRecall(retrievedKeys, tc.ExpectedChunks)
	mrr := MRR(retrievedKeys, tc.ExpectedChunks)
	ndcg := NDCG(retrievedKeys, tc.ExpectedChunks)

	var answer string
	var answerRelevance float64
	var faithfulness float64
	var contextRecall float64
	var answerExpected int
	var answerMatched int
	var answerCoverage float64
	var answerMissing []string
	judged := false
	if e.judge != nil {
		answerText, err := e.generateAnswer(ctx, tc.Query, resp.Results)
		if err != nil {
			return CaseResult{}, err
		}
		answer = answerText
		answerRelevance, err = e.judge.JudgeRelevance(ctx, tc.Query, answer)
		if err != nil {
			return CaseResult{}, err
		}
		faithfulness, err = e.judge.JudgeFaithfulness(ctx, answer, resp.Results)
		if err != nil {
			return CaseResult{}, err
		}
		contextRecall, err = e.judge.JudgeContextRecall(ctx, answer, resp.Results)
		if err != nil {
			return CaseResult{}, err
		}
		if len(tc.ExpectedAnswerContains) > 0 {
			answerExpected, answerMatched, answerMissing = MatchExpectedAnswer(answer, tc.ExpectedAnswerContains)
			if answerExpected > 0 {
				answerCoverage = float64(answerMatched) / float64(answerExpected)
			}
		}
		judged = true
	}

	return CaseResult{
		CaseID:         tc.ID,
		Query:          tc.Query,
		Retrieved:      len(retrievedKeys),
		Expected:       len(tc.ExpectedChunks),
		Precision:      precision,
		Recall:         recall,
		MRR:            mrr,
		NDCG:           ndcg,
		QueryTime:      resp.QueryTime,
		Answer:         answer,
		Relevance:      answerRelevance,
		Faithfulness:   faithfulness,
		ContextRecall:  contextRecall,
		Judged:         judged,
		AnswerExpected: answerExpected,
		AnswerMatched:  answerMatched,
		AnswerCoverage: answerCoverage,
		AnswerMissing:  answerMissing,
		ExpectedHints:  tc.ExpectedChunks,
	}, nil
}

func (e *Evaluator) generateAnswer(ctx context.Context, query string, results []*models.DocumentSearchResult) (string, error) {
	if e.judge == nil {
		return "", nil
	}
	context := BuildContext(results)
	return e.judge.GenerateAnswer(ctx, query, context, e.options.Model, e.options.MaxTokens)
}
