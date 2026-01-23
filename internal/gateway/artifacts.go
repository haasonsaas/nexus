package gateway

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/haasonsaas/nexus/internal/artifacts"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/sessions"
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
		if closer, ok := repo.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
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

	metadataBackend := strings.ToLower(strings.TrimSpace(cfg.Artifacts.MetadataBackend))
	if metadataBackend == "" {
		metadataBackend = "file"
	}

	switch metadataBackend {
	case "file":
		metadataPath := strings.TrimSpace(cfg.Artifacts.MetadataPath)
		if metadataPath == "" {
			metadataPath = filepath.Join(cfg.Artifacts.LocalPath, "metadata.json")
		}
		// NewPersistentRepository closes the store on failure
		return artifacts.NewPersistentRepository(store, metadataPath, logger)
	case "database", "db":
		db, err := openArtifactDB(cfg)
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		repo, err := artifacts.NewSQLRepository(db, store, logger)
		if err != nil {
			_ = db.Close()
			_ = store.Close()
			return nil, err
		}
		return repo, nil
	default:
		_ = store.Close()
		return nil, fmt.Errorf("unsupported artifacts metadata backend %q", metadataBackend)
	}
}

func openArtifactDB(cfg *config.Config) (*sql.DB, error) {
	if cfg == nil || strings.TrimSpace(cfg.Database.URL) == "" {
		return nil, fmt.Errorf("database url is required for artifacts metadata")
	}
	db, err := sql.Open("postgres", cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	pool := sessions.DefaultCockroachConfig()
	if cfg.Database.MaxConnections > 0 {
		pool.MaxOpenConns = cfg.Database.MaxConnections
	}
	if cfg.Database.ConnMaxLifetime > 0 {
		pool.ConnMaxLifetime = cfg.Database.ConnMaxLifetime
	}
	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	db.SetConnMaxIdleTime(pool.ConnMaxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), pool.ConnectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}
