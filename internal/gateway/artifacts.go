package gateway

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/haasonsaas/nexus/internal/artifacts"
	"github.com/haasonsaas/nexus/internal/config"
)

type artifactSetup struct {
	repo     artifacts.Repository
	redactor *artifacts.RedactionPolicy
	cleanup  *artifacts.CleanupService
}

func buildArtifactSetup(cfg *config.Config, logger *slog.Logger) (*artifactSetup, error) {
	if cfg == nil {
		return nil, nil
	}
	backend := strings.ToLower(strings.TrimSpace(cfg.Artifacts.Backend))
	if backend == "" || backend == "none" || backend == "disabled" {
		return nil, nil
	}

	var store artifacts.Store
	switch backend {
	case "local":
		localStore, err := artifacts.NewLocalStore(cfg.Artifacts.LocalPath)
		if err != nil {
			return nil, err
		}
		store = localStore
	case "s3", "minio":
		return nil, fmt.Errorf("artifact backend %q not implemented", backend)
	default:
		return nil, fmt.Errorf("unsupported artifact backend %q", backend)
	}

	repo := artifacts.NewMemoryRepository(store, logger)
	policy, err := artifacts.NewRedactionPolicy(artifacts.RedactionConfig{
		Enabled:          cfg.Artifacts.Redaction.Enabled,
		Types:            cfg.Artifacts.Redaction.Types,
		MimeTypes:        cfg.Artifacts.Redaction.MimeTypes,
		FilenamePatterns: cfg.Artifacts.Redaction.FilenamePatterns,
	})
	if err != nil {
		return nil, err
	}

	cleanup := artifacts.NewCleanupService(repo, cfg.Artifacts.PruneInterval, logger)

	return &artifactSetup{
		repo:     repo,
		redactor: policy,
		cleanup:  cleanup,
	}, nil
}
