package artifacts

import (
	"context"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3StoreConfig configures the S3/MinIO store.
type S3StoreConfig struct {
	// Endpoint is the S3-compatible endpoint URL (e.g., "http://localhost:9000" for MinIO)
	Endpoint string

	// Region is the AWS region (e.g., "us-east-1")
	Region string

	// Bucket is the S3 bucket name
	Bucket string

	// Prefix is an optional path prefix for all objects
	Prefix string

	// AccessKeyID is the AWS access key ID
	AccessKeyID string

	// SecretAccessKey is the AWS secret access key
	SecretAccessKey string

	// UsePathStyle enables path-style addressing (required for MinIO and some S3-compatible services)
	UsePathStyle bool

	// PresignExpiry is the default expiry for presigned URLs
	PresignExpiry time.Duration
}

// DefaultS3StoreConfig returns the default configuration.
func DefaultS3StoreConfig() *S3StoreConfig {
	return &S3StoreConfig{
		Region:        "us-east-1",
		UsePathStyle:  true,
		PresignExpiry: 1 * time.Hour,
	}
}

// S3Store implements Store using S3 or S3-compatible storage (MinIO, etc.).
type S3Store struct {
	client        *s3.Client
	presignClient *s3.PresignClient
	bucket        string
	prefix        string
	presignExpiry time.Duration
}

// NewS3Store creates a new S3-backed store.
func NewS3Store(ctx context.Context, cfg *S3StoreConfig) (*S3Store, error) {
	if cfg == nil {
		cfg = DefaultS3StoreConfig()
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("bucket is required")
	}

	// Build AWS config options
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}

	// Add credentials if provided
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	// Create S3 client with optional endpoint
	s3Opts := func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	}

	client := s3.NewFromConfig(awsCfg, s3Opts)
	presignClient := s3.NewPresignClient(client)

	presignExpiry := cfg.PresignExpiry
	if presignExpiry == 0 {
		presignExpiry = 1 * time.Hour
	}

	return &S3Store{
		client:        client,
		presignClient: presignClient,
		bucket:        cfg.Bucket,
		prefix:        cfg.Prefix,
		presignExpiry: presignExpiry,
	}, nil
}

// Put stores artifact data in S3.
func (s *S3Store) Put(ctx context.Context, artifactID string, data io.Reader, opts PutOptions) (string, error) {
	key := s.objectKey(artifactID, opts)

	contentType := opts.MimeType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        data,
		ContentType: aws.String(contentType),
		Metadata:    opts.Metadata,
	})
	if err != nil {
		return "", fmt.Errorf("put object: %w", err)
	}

	// Return s3:// reference
	return fmt.Sprintf("s3://%s/%s", s.bucket, key), nil
}

// Get retrieves artifact data from S3.
func (s *S3Store) Get(ctx context.Context, artifactID string) (io.ReadCloser, error) {
	// Try common key patterns
	keys := []string{
		s.keyWithPrefix(artifactID),
		artifactID,
	}

	var lastErr error
	for _, key := range keys {
		result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(key),
		})
		if err == nil {
			return result.Body, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("get object: %w", lastErr)
}

// Delete removes an artifact from S3.
func (s *S3Store) Delete(ctx context.Context, artifactID string) error {
	key := s.keyWithPrefix(artifactID)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

// Exists checks if an artifact exists in S3.
func (s *S3Store) Exists(ctx context.Context, artifactID string) (bool, error) {
	key := s.keyWithPrefix(artifactID)

	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// Check if it's a not found error
		return false, nil
	}
	return true, nil
}

// Close releases any resources.
func (s *S3Store) Close() error {
	return nil
}

// PresignedURL generates a presigned URL for downloading an artifact.
func (s *S3Store) PresignedURL(ctx context.Context, artifactID string, expiry time.Duration) (string, error) {
	key := s.keyWithPrefix(artifactID)

	if expiry == 0 {
		expiry = s.presignExpiry
	}

	req, err := s.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("presign URL: %w", err)
	}

	return req.URL, nil
}

// objectKey generates the S3 object key for an artifact.
func (s *S3Store) objectKey(artifactID string, opts PutOptions) string {
	// Generate path: prefix/type/YYYY/MM/DD/artifactID.ext
	now := time.Now()
	artifactType := "unknown"
	if t, ok := opts.Metadata["type"]; ok {
		artifactType = t
	}

	ext := extensionForMime(opts.MimeType)
	filename := artifactID + ext

	parts := []string{}
	if s.prefix != "" {
		parts = append(parts, s.prefix)
	}
	parts = append(parts, artifactType,
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", now.Month()),
		fmt.Sprintf("%02d", now.Day()),
		filename)

	return path.Join(parts...)
}

// keyWithPrefix returns the object key with prefix.
func (s *S3Store) keyWithPrefix(key string) string {
	if s.prefix == "" {
		return key
	}
	return path.Join(s.prefix, key)
}
