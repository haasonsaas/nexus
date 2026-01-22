package artifacts

import (
	"context"
	"log/slog"
	"time"
)

// CleanupService periodically removes expired artifacts.
type CleanupService struct {
	repo     Repository
	interval time.Duration
	logger   *slog.Logger
	stopCh   chan struct{}
}

// NewCleanupService creates a cleanup service.
func NewCleanupService(repo Repository, interval time.Duration, logger *slog.Logger) *CleanupService {
	if interval == 0 {
		interval = time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &CleanupService{
		repo:     repo,
		interval: interval,
		logger:   logger,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the cleanup loop.
func (s *CleanupService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.logger.Info("artifact cleanup service started", "interval", s.interval)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("artifact cleanup service stopping (context)")
			return
		case <-s.stopCh:
			s.logger.Info("artifact cleanup service stopping (signal)")
			return
		case <-ticker.C:
			count, err := s.repo.PruneExpired(ctx)
			if err != nil {
				s.logger.Error("artifact cleanup failed", "error", err)
			} else if count > 0 {
				s.logger.Info("artifact cleanup completed", "pruned", count)
			}
		}
	}
}

// Stop signals the cleanup service to stop.
func (s *CleanupService) Stop() {
	close(s.stopCh)
}
