package storage

import "time"

// CockroachConfig configures connection pooling for CockroachDB.
type CockroachConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	ConnectTimeout  time.Duration
}

// DefaultCockroachConfig returns default connection pool settings.
func DefaultCockroachConfig() *CockroachConfig {
	return &CockroachConfig{
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
		ConnectTimeout:  10 * time.Second,
	}
}
