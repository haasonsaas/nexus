package eval

import (
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/pkg/models"
)

// BuildContext formats retrieved chunks into a plain-text context block for evaluation.
func BuildContext(results []*models.DocumentSearchResult) string {
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Retrieved Context:\n\n")
	for i, result := range results {
		if result == nil || result.Chunk == nil {
			continue
		}
		chunk := result.Chunk
		meta := chunk.Metadata
		source := strings.TrimSpace(meta.DocumentName)
		if source == "" {
			source = strings.TrimSpace(chunk.DocumentID)
		}
		if source == "" {
			source = "Document"
		}
		sb.WriteString(fmt.Sprintf("[%d] %s", i+1, source))
		if meta.Section != "" {
			sb.WriteString(" / ")
			sb.WriteString(meta.Section)
		}
		if chunk.DocumentID != "" && chunk.DocumentID != source {
			sb.WriteString(" (id: ")
			sb.WriteString(chunk.DocumentID)
			sb.WriteString(")")
		}
		if result.Score > 0 {
			sb.WriteString(fmt.Sprintf(" (score: %.2f)", result.Score))
		}
		sb.WriteString("\n")
		sb.WriteString(chunk.Content)
		sb.WriteString("\n\n")
	}
	return strings.TrimSpace(sb.String())
}
