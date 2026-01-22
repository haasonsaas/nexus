package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/rag/index"
	"github.com/haasonsaas/nexus/pkg/models"
)

// UploadTool implements agent.Tool for uploading documents to the RAG system.
type UploadTool struct {
	manager *index.Manager
	config  UploadToolConfig
}

// UploadToolConfig configures the document upload tool.
type UploadToolConfig struct {
	// MaxContentLength is the maximum content length allowed.
	// Default: 100000 (100KB of text)
	MaxContentLength int

	// DefaultSource is the default source for uploaded documents.
	// Default: "tool_upload"
	DefaultSource string

	// AllowedContentTypes restricts which content types can be uploaded.
	// Empty means all types are allowed.
	AllowedContentTypes []string
}

// DefaultUploadToolConfig returns the default upload tool configuration.
func DefaultUploadToolConfig() UploadToolConfig {
	return UploadToolConfig{
		MaxContentLength: 100000,
		DefaultSource:    "tool_upload",
	}
}

// NewUploadTool creates a new document upload tool.
func NewUploadTool(manager *index.Manager, cfg *UploadToolConfig) *UploadTool {
	config := DefaultUploadToolConfig()
	if cfg != nil {
		if cfg.MaxContentLength > 0 {
			config.MaxContentLength = cfg.MaxContentLength
		}
		if cfg.DefaultSource != "" {
			config.DefaultSource = cfg.DefaultSource
		}
		config.AllowedContentTypes = cfg.AllowedContentTypes
	}

	return &UploadTool{
		manager: manager,
		config:  config,
	}
}

// Name returns the tool name.
func (t *UploadTool) Name() string {
	return "document_upload"
}

// Description returns the tool description.
func (t *UploadTool) Description() string {
	return "Uploads and indexes a document for later retrieval. Use this to add new documents, notes, or reference materials to the knowledge base."
}

// Schema returns the JSON schema for tool parameters.
func (t *UploadTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Name or title of the document"
    },
    "content": {
      "type": "string",
      "description": "The text content of the document"
    },
    "content_type": {
      "type": "string",
      "description": "MIME type of the content (default: text/plain)",
      "enum": ["text/plain", "text/markdown"]
    },
    "tags": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Tags for categorizing the document"
    },
    "description": {
      "type": "string",
      "description": "Brief description of the document"
    }
  },
  "required": ["name", "content"]
}`)
}

// uploadInput represents the tool input parameters.
type uploadInput struct {
	Name        string   `json:"name"`
	Content     string   `json:"content"`
	ContentType string   `json:"content_type,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Description string   `json:"description,omitempty"`
}

// uploadOutput represents the upload result.
type uploadOutput struct {
	DocumentID  string `json:"document_id"`
	Name        string `json:"name"`
	ChunkCount  int    `json:"chunk_count"`
	TotalTokens int    `json:"total_tokens"`
	Message     string `json:"message"`
}

// Execute runs the document upload.
func (t *UploadTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input uploadInput
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid parameters: %v", err),
			IsError: true,
		}, nil
	}

	// Validate name
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return &agent.ToolResult{
			Content: "Document name is required",
			IsError: true,
		}, nil
	}

	// Validate content
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return &agent.ToolResult{
			Content: "Document content is required",
			IsError: true,
		}, nil
	}

	// Check content length
	if len(content) > t.config.MaxContentLength {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Content exceeds maximum length of %d characters", t.config.MaxContentLength),
			IsError: true,
		}, nil
	}

	// Determine content type
	contentType := input.ContentType
	if contentType == "" {
		contentType = "text/plain"
	}

	// Check allowed content types
	if len(t.config.AllowedContentTypes) > 0 {
		allowed := false
		for _, ct := range t.config.AllowedContentTypes {
			if ct == contentType {
				allowed = true
				break
			}
		}
		if !allowed {
			return &agent.ToolResult{
				Content: fmt.Sprintf("Content type %q is not allowed", contentType),
				IsError: true,
			}, nil
		}
	}

	// Build metadata
	metadata := &models.DocumentMetadata{
		Title:       name,
		Description: input.Description,
		Tags:        input.Tags,
	}

	// Index the document
	req := &index.IndexRequest{
		Name:        name,
		Source:      t.config.DefaultSource,
		ContentType: contentType,
		Content:     strings.NewReader(content),
		Metadata:    metadata,
	}

	result, err := t.manager.Index(ctx, req)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to index document: %v", err),
			IsError: true,
		}, nil
	}

	// Format output
	output := uploadOutput{
		DocumentID:  result.Document.ID,
		Name:        result.Document.Name,
		ChunkCount:  result.ChunkCount,
		TotalTokens: result.TotalTokens,
		Message:     fmt.Sprintf("Successfully indexed document %q with %d chunks", name, result.ChunkCount),
	}

	outputJSON, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to format result: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: string(outputJSON),
	}, nil
}

// ListTool implements agent.Tool for listing indexed documents.
type ListTool struct {
	manager *index.Manager
}

// NewListTool creates a new document list tool.
func NewListTool(manager *index.Manager) *ListTool {
	return &ListTool{manager: manager}
}

// Name returns the tool name.
func (t *ListTool) Name() string {
	return "document_list"
}

// Description returns the tool description.
func (t *ListTool) Description() string {
	return "Lists documents in the knowledge base with optional filtering."
}

// Schema returns the JSON schema for tool parameters.
func (t *ListTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "limit": {
      "type": "integer",
      "description": "Maximum number of documents to return (default: 20)"
    },
    "source": {
      "type": "string",
      "description": "Filter by document source"
    },
    "tags": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Filter by tags"
    }
  }
}`)
}

// listInput represents the tool input parameters.
type listInput struct {
	Limit  int      `json:"limit,omitempty"`
	Source string   `json:"source,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

// listDocOutput represents a document in the list.
type listDocOutput struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	ChunkCount int      `json:"chunk_count"`
	Tags       []string `json:"tags,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

// Execute runs the document list.
func (t *ListTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input listInput
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid parameters: %v", err),
			IsError: true,
		}, nil
	}

	// Apply defaults
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// List documents
	docs, err := t.manager.ListDocuments(ctx, nil)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to list documents: %v", err),
			IsError: true,
		}, nil
	}

	// Filter and format
	results := make([]listDocOutput, 0)
	for _, doc := range docs {
		if len(results) >= limit {
			break
		}

		// Apply source filter
		if input.Source != "" && doc.Source != input.Source {
			continue
		}

		// Apply tags filter
		if len(input.Tags) > 0 {
			hasTag := false
			for _, tag := range input.Tags {
				for _, docTag := range doc.Metadata.Tags {
					if tag == docTag {
						hasTag = true
						break
					}
				}
				if hasTag {
					break
				}
			}
			if !hasTag {
				continue
			}
		}

		results = append(results, listDocOutput{
			ID:         doc.ID,
			Name:       doc.Name,
			Source:     doc.Source,
			ChunkCount: doc.ChunkCount,
			Tags:       doc.Metadata.Tags,
			CreatedAt:  doc.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	// Format output
	outputJSON, err := json.MarshalIndent(struct {
		Count     int             `json:"count"`
		Documents []listDocOutput `json:"documents"`
	}{
		Count:     len(results),
		Documents: results,
	}, "", "  ")
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to format results: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: string(outputJSON),
	}, nil
}

// DeleteTool implements agent.Tool for deleting documents.
type DeleteTool struct {
	manager *index.Manager
}

// NewDeleteTool creates a new document delete tool.
func NewDeleteTool(manager *index.Manager) *DeleteTool {
	return &DeleteTool{manager: manager}
}

// Name returns the tool name.
func (t *DeleteTool) Name() string {
	return "document_delete"
}

// Description returns the tool description.
func (t *DeleteTool) Description() string {
	return "Deletes a document from the knowledge base by ID."
}

// Schema returns the JSON schema for tool parameters.
func (t *DeleteTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "document_id": {
      "type": "string",
      "description": "The ID of the document to delete"
    }
  },
  "required": ["document_id"]
}`)
}

// Execute runs the document delete.
func (t *DeleteTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		DocumentID string `json:"document_id"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Invalid parameters: %v", err),
			IsError: true,
		}, nil
	}

	docID := strings.TrimSpace(input.DocumentID)
	if docID == "" {
		return &agent.ToolResult{
			Content: "Document ID is required",
			IsError: true,
		}, nil
	}

	// Check if document exists
	doc, err := t.manager.GetDocument(ctx, docID)
	if err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to find document: %v", err),
			IsError: true,
		}, nil
	}
	if doc == nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Document not found: %s", docID),
			IsError: true,
		}, nil
	}

	// Delete the document
	if err := t.manager.DeleteDocument(ctx, docID); err != nil {
		return &agent.ToolResult{
			Content: fmt.Sprintf("Failed to delete document: %v", err),
			IsError: true,
		}, nil
	}

	return &agent.ToolResult{
		Content: fmt.Sprintf("Successfully deleted document %q (ID: %s)", doc.Name, docID),
	}, nil
}
