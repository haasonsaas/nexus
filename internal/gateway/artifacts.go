package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		s3Cfg := &artifacts.S3StoreConfig{
			Endpoint:        cfg.Artifacts.S3Endpoint,
			Region:          cfg.Artifacts.S3Region,
			Bucket:          cfg.Artifacts.S3Bucket,
			Prefix:          cfg.Artifacts.S3Prefix,
			AccessKeyID:     cfg.Artifacts.S3AccessKeyID,
			SecretAccessKey: cfg.Artifacts.S3SecretAccessKey,
			UsePathStyle:    backend == "minio", // MinIO requires path-style
		}
		if s3Cfg.Region == "" {
			s3Cfg.Region = "us-east-1"
		}

		s3Store, err := artifacts.NewS3Store(ctx, s3Cfg)
		if err != nil {
			return nil, fmt.Errorf("create S3 store: %w", err)
		}
		store = s3Store
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
