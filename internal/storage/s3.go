package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Config holds the configuration for S3 storage.
type S3Config struct {
	Bucket          string
	Region          string
	Endpoint        string // Optional: for custom S3-compatible endpoints
	AccessKeyID     string // Optional: AWS access key ID
	SecretAccessKey string // Optional: AWS secret access key
}

// S3Storage wraps LocalStorage and adds S3 upload capability.
// It uses LocalStorage for temporary file operations and S3 for final storage.
type S3Storage struct {
	*LocalStorage
	client *s3.Client
	bucket string
	region string
}

// NewS3Storage creates a new S3Storage instance.
// The tempDir parameter specifies where temporary files are stored.
// The cfg parameter contains S3 configuration.
func NewS3Storage(tempDir string, cfg S3Config) (*S3Storage, error) {
	local, err := NewLocalStorage(tempDir)
	if err != nil {
		return nil, err
	}

	var configOpts []func(*config.LoadOptions) error
	configOpts = append(configOpts, config.WithRegion(cfg.Region))

	// Use static credentials if provided
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		configOpts = append(configOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background(), configOpts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	var clientOpts []func(*s3.Options)
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, clientOpts...)

	return &S3Storage{
		LocalStorage: local,
		client:       client,
		bucket:       cfg.Bucket,
		region:       cfg.Region,
	}, nil
}

// UploadToS3 uploads data to S3 and returns the public URL.
func (s *S3Storage) UploadToS3(ctx context.Context, key string, data io.Reader) (string, error) {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   data,
	})
	if err != nil {
		return "", fmt.Errorf("upload to S3: %w", err)
	}

	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, s.region, key)
	return url, nil
}
