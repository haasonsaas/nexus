package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
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
	repo, err := BuildArtifactRepository(context.Background(), cfg, logger)
	if err != nil || repo == nil {
		return nil, err
	}
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

// BuildArtifactRepository constructs the artifact repository based on config.
func BuildArtifactRepository(ctx context.Context, cfg *config.Config, logger *slog.Logger) (artifacts.Repository, error) {
	if cfg == nil {
		return nil, nil
	}
	backend := strings.ToLower(strings.TrimSpace(cfg.Artifacts.Backend))
	if backend == "" || backend == "none" || backend == "disabled" {
		return nil, nil
	}
	if cfg.Artifacts.TTLs != nil {
		artifacts.SetDefaultTTLs(cfg.Artifacts.TTLs)
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
		usePathStyle := backend == "minio"
		if strings.TrimSpace(cfg.Artifacts.S3Endpoint) != "" {
			usePathStyle = true
		}
		s3Store, err := artifacts.NewS3Store(ctx, &artifacts.S3StoreConfig{
			Bucket:          cfg.Artifacts.S3Bucket,
			Region:          cfg.Artifacts.S3Region,
			Endpoint:        cfg.Artifacts.S3Endpoint,
			Prefix:          cfg.Artifacts.S3Prefix,
			AccessKeyID:     cfg.Artifacts.S3AccessKeyID,
			SecretAccessKey: cfg.Artifacts.S3SecretAccessKey,
			UsePathStyle:    usePathStyle,
		})
		if err != nil {
			return nil, err
		}
		store = s3Store
	default:
		return nil, fmt.Errorf("unsupported artifact backend %q", backend)
	}

	metadataPath := strings.TrimSpace(cfg.Artifacts.MetadataPath)
	if metadataPath == "" {
		metadataPath = filepath.Join(cfg.Artifacts.LocalPath, "metadata.json")
	}
	repo, err := artifacts.NewPersistentRepository(store, metadataPath, logger)
	if err != nil {
		return nil, err
	}
	return repo, nil
}
