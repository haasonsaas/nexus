package eval

import (
	"math"
	"testing"
)

func TestPrecisionRecallMRRNDCG(t *testing.T) {
	expected := []ExpectedChunk{
		{DocID: "doc1", Section: "A"},
		{DocID: "doc2", Section: "B"},
	}
	retrieved := []ResultKey{
		{DocID: "doc1", Section: "A"},
		{DocID: "doc3", Section: "X"},
		{DocID: "doc2", Section: "B"},
	}
	precision, recall := PrecisionRecall(retrieved, expected)
	if math.Abs(precision-(2.0/3.0)) > 1e-6 {
		t.Errorf("precision = %v", precision)
	}
	if math.Abs(recall-1.0) > 1e-6 {
		t.Errorf("recall = %v", recall)
	}
	mrr := MRR(retrieved, expected)
	if math.Abs(mrr-1.0) > 1e-6 {
		t.Errorf("mrr = %v", mrr)
	}
	ndcg := NDCG(retrieved, expected)
	// Expected ~0.9197
	if math.Abs(ndcg-0.9197) > 1e-3 {
		t.Errorf("ndcg = %v", ndcg)
	}
}

func TestDocOnlyMatch(t *testing.T) {
	expected := []ExpectedChunk{{DocID: "doc1"}}
	retrieved := []ResultKey{{DocID: "doc1", Section: "Intro"}}
	precision, recall := PrecisionRecall(retrieved, expected)
	if precision != 1 || recall != 1 {
		t.Errorf("doc-only match failed: precision=%v recall=%v", precision, recall)
	}
}

func TestMatchExpectedAnswer(t *testing.T) {
	answer := "Configure MCP.Servers with transport http and TLS."
	expected := []string{"mcp.servers", "transport", "missing", ""}
	expectedCount, matchedCount, missing := MatchExpectedAnswer(answer, expected)
	if expectedCount != 3 {
		t.Fatalf("expected count = %d", expectedCount)
	}
	if matchedCount != 2 {
		t.Fatalf("matched count = %d", matchedCount)
	}
	if len(missing) != 1 || missing[0] != "missing" {
		t.Fatalf("missing = %v", missing)
	}
}
