package artifacts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3StoreConfig configures an S3-compatible artifact store.
type S3StoreConfig struct {
	Bucket          string
	Region          string
	Endpoint        string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
}

// DefaultS3StoreConfig returns the default configuration.
func DefaultS3StoreConfig() *S3StoreConfig {
	return &S3StoreConfig{
		Region: "us-east-1",
	}
}

// S3Store stores artifacts in an S3-compatible bucket.
type S3Store struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3Store creates a new S3-backed artifact store.
func NewS3Store(ctx context.Context, cfg *S3StoreConfig) (*S3Store, error) {
	if cfg == nil {
		cfg = DefaultS3StoreConfig()
	}

	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, fmt.Errorf("s3 bucket is required")
	}
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}

	loadOptions := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		loadOptions = append(loadOptions, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		if cfg.UsePathStyle {
			o.UsePathStyle = true
		}
	})

	prefix := strings.Trim(cfg.Prefix, "/")
	return &S3Store{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}, nil
}

// Put stores artifact data in S3.
func (s *S3Store) Put(ctx context.Context, artifactID string, data io.Reader, opts PutOptions) (string, error) {
	key := s.objectKey(artifactID)
	input := &s3.PutObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
		Body:   data,
	}
	if opts.MimeType != "" {
		input.ContentType = aws.String(opts.MimeType)
	}
	if len(opts.Metadata) > 0 {
		input.Metadata = opts.Metadata
	}
	if _, err := s.client.PutObject(ctx, input); err != nil {
		return "", fmt.Errorf("s3 put object: %w", err)
	}

	return fmt.Sprintf("s3://%s/%s", s.bucket, key), nil
}

// Get retrieves artifact data from S3.
func (s *S3Store) Get(ctx context.Context, artifactID string) (io.ReadCloser, error) {
	key := s.objectKey(artifactID)
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get object: %w", err)
	}
	return out.Body, nil
}

// Delete removes an artifact from S3.
func (s *S3Store) Delete(ctx context.Context, artifactID string) error {
	key := s.objectKey(artifactID)
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}); err != nil {
		return fmt.Errorf("s3 delete object: %w", err)
	}
	return nil
}

// Exists checks if an artifact exists in S3.
func (s *S3Store) Exists(ctx context.Context, artifactID string) (bool, error) {
	key := s.objectKey(artifactID)
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err == nil {
		return true, nil
	}
	var notFound *types.NotFound
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &notFound) || errors.As(err, &noSuchKey) {
		return false, nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && strings.EqualFold(apiErr.ErrorCode(), "NotFound") {
		return false, nil
	}
	return false, fmt.Errorf("s3 head object: %w", err)
}

// Close releases resources.
func (s *S3Store) Close() error {
	return nil
}

func (s *S3Store) objectKey(artifactID string) string {
	if s.prefix == "" {
		return artifactID
	}
	return path.Join(s.prefix, artifactID)
}
