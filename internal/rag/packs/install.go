package packs

import (
	"context"
	"crypto/sha1"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/rag/index"
	"github.com/haasonsaas/nexus/pkg/models"
)

// InstallReport summarizes a pack installation.
type InstallReport struct {
	PackName  string        `json:"pack_name"`
	Documents int           `json:"documents"`
	Chunks    int           `json:"chunks"`
	Duration  time.Duration `json:"duration"`
	Errors    []string      `json:"errors,omitempty"`
}

// Install indexes a knowledge pack into the RAG index.
func Install(ctx context.Context, dir string, idx *index.Manager) (*InstallReport, error) {
	if idx == nil {
		return nil, fmt.Errorf("index manager is required")
	}
	pack, err := LoadFromDir(dir)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	report := &InstallReport{PackName: pack.Name}

	for _, doc := range pack.Documents {
		if doc.Path == "" {
			report.Errors = append(report.Errors, "document path is required")
			continue
		}
		name := strings.TrimSpace(doc.Name)
		if name == "" {
			name = filepath.Base(doc.Path)
		}
		absPath := filepath.Join(dir, doc.Path)
		file, err := os.Open(absPath)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("open %s: %v", doc.Path, err))
			continue
		}

		contentType := strings.TrimSpace(doc.ContentType)
		if contentType == "" {
			contentType = detectContentType(absPath)
		}
		source := strings.TrimSpace(doc.Source)
		if source == "" {
			source = "pack:" + pack.Name
		}
		docID := deterministicDocID(pack.Name, doc.Path)

		meta := &models.DocumentMetadata{
			Title: name,
			Tags:  doc.Tags,
		}

		result, err := idx.Index(ctx, &index.IndexRequest{
			DocumentID:  docID,
			Name:        name,
			Source:      source,
			SourceURI:   absPath,
			ContentType: contentType,
			Content:     file,
			Metadata:    meta,
		})
		file.Close()
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("index %s: %v", doc.Path, err))
			continue
		}
		report.Documents++
		report.Chunks += result.ChunkCount
	}

	report.Duration = time.Since(start)
	return report, nil
}

func deterministicDocID(packName, path string) string {
	h := sha1.Sum([]byte(packName + ":" + path))
	return fmt.Sprintf("pack:%s:%x", packName, h[:6])
}

func detectContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != "" {
		if ct := mime.TypeByExtension(ext); ct != "" {
			if semi := strings.Index(ct, ";"); semi != -1 {
				ct = ct[:semi]
			}
			return ct
		}
	}
	return "text/plain"
}
