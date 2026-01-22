package media

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Aggregator processes multiple attachments concurrently.
type Aggregator struct {
	processor   Processor
	concurrency int
	logger      *slog.Logger
}

// NewAggregator creates a new media aggregator.
func NewAggregator(processor Processor, logger *slog.Logger) *Aggregator {
	if processor == nil {
		processor = NewDefaultProcessor(logger)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Aggregator{
		processor:   processor,
		concurrency: 4, // Default concurrent processing
		logger:      logger.With("component", "media-aggregator"),
	}
}

// SetConcurrency sets the maximum concurrent processing operations.
func (a *Aggregator) SetConcurrency(n int) {
	if n > 0 {
		a.concurrency = n
	}
}

// ProcessAll processes multiple attachments and returns all results.
func (a *Aggregator) ProcessAll(ctx context.Context, attachments []*Attachment, opts ProcessingOptions) []*ProcessingResult {
	if len(attachments) == 0 {
		return nil
	}

	results := make([]*ProcessingResult, len(attachments))

	// Use semaphore for concurrency control
	sem := make(chan struct{}, a.concurrency)
	var wg sync.WaitGroup

	for i, attachment := range attachments {
		wg.Add(1)
		go func(idx int, att *Attachment) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = &ProcessingResult{
					Attachment: att,
					Error:      ctx.Err().Error(),
				}
				return
			}

			result, err := a.processor.Process(att, opts)
			if err != nil {
				a.logger.Warn("attachment processing failed",
					"id", att.ID,
					"type", att.Type,
					"error", err)
			}
			results[idx] = result
		}(i, attachment)
	}

	wg.Wait()
	return results
}

// AggregatedContent holds all processed content from attachments.
type AggregatedContent struct {
	// Images are base64-encoded images ready for vision models
	Images []Content `json:"images,omitempty"`

	// Text is aggregated text content (transcriptions, descriptions)
	Text string `json:"text,omitempty"`

	// Errors lists any processing errors
	Errors []string `json:"errors,omitempty"`

	// ProcessedCount is the number of successfully processed attachments
	ProcessedCount int `json:"processed_count"`

	// TotalCount is the total number of attachments
	TotalCount int `json:"total_count"`
}

// Aggregate processes attachments and aggregates the results.
func (a *Aggregator) Aggregate(ctx context.Context, attachments []*Attachment, opts ProcessingOptions) *AggregatedContent {
	results := a.ProcessAll(ctx, attachments, opts)

	content := &AggregatedContent{
		Images:     make([]Content, 0),
		Errors:     make([]string, 0),
		TotalCount: len(attachments),
	}

	var textParts []string

	for _, result := range results {
		if result == nil {
			continue
		}

		if result.Error != "" {
			content.Errors = append(content.Errors, result.Error)
			continue
		}

		content.ProcessedCount++

		for _, c := range result.Contents {
			switch c.Type {
			case ContentTypeImage:
				content.Images = append(content.Images, c)
			case ContentTypeText:
				if c.Text != "" {
					textParts = append(textParts, c.Text)
				}
			}
		}

		if result.Transcription != "" {
			textParts = append(textParts, fmt.Sprintf("[Transcription]: %s", result.Transcription))
		}
		if result.Description != "" {
			textParts = append(textParts, fmt.Sprintf("[Description]: %s", result.Description))
		}
	}

	if len(textParts) > 0 {
		content.Text = joinWithNewlines(textParts)
	}

	return content
}

// HasContent checks if the aggregated content has any usable content.
func (c *AggregatedContent) HasContent() bool {
	return len(c.Images) > 0 || c.Text != ""
}

// HasErrors checks if there were any processing errors.
func (c *AggregatedContent) HasErrors() bool {
	return len(c.Errors) > 0
}

func joinWithNewlines(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += "\n\n" + parts[i]
	}
	return result
}
