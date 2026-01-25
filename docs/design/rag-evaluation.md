# RAG Evaluation Pipeline Design

## Overview

This document specifies an evaluation pipeline for the RAG system, covering retrieval metrics, chunk quality, and LLM-judged answer quality. It addresses issue #96.

## Goals

1. **Quantify retrieval quality** (precision, recall, MRR, NDCG).
2. **Measure chunk quality** (size, overlap, coherence).
3. **Evaluate end-to-end answer quality** (relevance, faithfulness).
4. **Enable continuous regression detection** via scheduled evals.

## Non-Goals

- Automated RAG tuning/optimization (future work).
- Human labeling workflows (optional, later).

---

## 1. Data Model

### 1.1 Test Set

```yaml
# evaluations/rag_test_set.yaml
version: 1
name: core-docs
cases:
  - id: test_001
    query: "How do I configure MCP servers?"
    expected_chunks:
      - doc_id: mcp_guide
        section: configuration
    expected_answer_contains:
      - "mcp.servers"
      - "transport"
```

### 1.2 Metrics

```go
type RAGMetrics struct {
    Precision float64
    Recall    float64
    MRR       float64
    NDCG      float64

    AvgChunkSize   int
    ChunkOverlap   float64
    ChunkCoherence float64

    AnswerRelevance float64
    Faithfulness    float64
    ContextRecall   float64
}
```

---

## 2. Evaluator

### 2.1 Flow

1. Run retrieval for each test case.
2. Compute retrieval metrics vs expected chunks.
3. Generate answer using retrieved context.
4. Use LLM-as-judge to score relevance/faithfulness.
5. Aggregate report.

### 2.2 Interfaces

```go
type Evaluator struct {
    index  *index.Manager
    llm    agent.LLMProvider
    judge  *LLMJudge
}

func (e *Evaluator) Evaluate(ctx context.Context, set *TestSet) (*Report, error)
```

---

## 3. LLM-as-Judge

- Use existing LLM provider interface.
- Prompts enforce numeric output (0-1).
- Run with deterministic settings (temperature 0).

---

## 4. CLI

```bash
nexus rag eval --test-set ./evaluations/rag_test_set.yaml --output report.json
nexus rag eval --test-set ./evaluations/rag_test_set.yaml --judge --judge-provider anthropic --judge-model claude-sonnet-4-20250514
nexus rag eval --query "How do I..." --expected-doc mcp_guide
```

---

## 5. Continuous Evaluation

Use cron integration:

```yaml
cron:
  jobs:
    - id: rag-eval
      schedule: "0 0 * * 0"
      handler: rag.evaluate
      config:
        test_set: ./evaluations/production_set.yaml
        alert_threshold: 0.8
```

---

## 6. Reporting

- JSON report with per-case metrics and aggregates.
- Optional markdown summary for quick inspection.

---

## 7. Rollout Plan

1. Phase 1: CLI evaluation runner + JSON report.
2. Phase 2: LLM judge metrics + chunk coherence.
3. Phase 3: Scheduled evaluations + alerts.

---

## Testing

- Unit tests for metric calculations.
- Golden tests for report output.
- Integration tests with mock index + mock LLM.
