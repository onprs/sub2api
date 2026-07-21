package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

type ticketAttachmentStoreRegistry struct {
	local     service.TicketAttachmentStore
	s3Factory service.TicketAttachmentS3StoreFactory
}

func NewTicketAttachmentStoreRegistry(cfg *config.Config, factory service.TicketAttachmentS3StoreFactory) (service.TicketAttachmentStoreRegistry, error) {
	local, err := newTicketAttachmentLocalStore(cfg)
	if err != nil {
		// Attachment uploads default to disabled. An unavailable local root must not
		// prevent the API from starting or historical S3 attachments from loading.
		return &ticketAttachmentStoreRegistry{s3Factory: factory}, nil
	}
	return &ticketAttachmentStoreRegistry{local: local, s3Factory: factory}, nil
}

func (r *ticketAttachmentStoreRegistry) LocalStore() service.TicketAttachmentStore {
	return r.local
}

func (r *ticketAttachmentStoreRegistry) S3Store(ctx context.Context, cfg service.TicketAttachmentS3Config) (service.TicketAttachmentStore, error) {
	if r.s3Factory == nil {
		return nil, domainStorageProviderUnavailable("S3 store factory is unavailable")
	}
	return r.s3Factory(ctx, cfg)
}

type ticketAttachmentS3Store struct {
	client *s3.Client
	bucket string
	prefix string
}

func NewTicketAttachmentS3StoreFactory() service.TicketAttachmentS3StoreFactory {
	return func(ctx context.Context, cfg service.TicketAttachmentS3Config) (service.TicketAttachmentStore, error) {
		region := strings.TrimSpace(cfg.Region)
		if region == "" {
			region = "auto"
		}
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion(region),
			awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
		)
		if err != nil {
			return nil, fmt.Errorf("load ticket attachment S3 config: %w", err)
		}
		client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
			if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
				options.BaseEndpoint = &endpoint
			}
			options.UsePathStyle = cfg.ForcePathStyle
			options.APIOptions = append(options.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware)
			options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		})
		return &ticketAttachmentS3Store{
			client: client,
			bucket: strings.TrimSpace(cfg.Bucket),
			prefix: strings.Trim(strings.TrimSpace(cfg.Prefix), "/"),
		}, nil
	}
}

func (s *ticketAttachmentS3Store) Provider() string { return "s3" }

func (s *ticketAttachmentS3Store) Put(ctx context.Context, objectKey string, data []byte, contentType string) error {
	key := s.key(objectKey)
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, Body: bytes.NewReader(data), ContentType: &contentType,
	})
	if err != nil {
		return fmt.Errorf("put ticket attachment S3 object: %w", err)
	}
	return nil
}

func (s *ticketAttachmentS3Store) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	key := s.key(objectKey)
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return nil, fmt.Errorf("get ticket attachment S3 object: %w", err)
	}
	return result.Body, nil
}

func (s *ticketAttachmentS3Store) Delete(ctx context.Context, objectKey string) error {
	key := s.key(objectKey)
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return fmt.Errorf("delete ticket attachment S3 object: %w", err)
	}
	return nil
}

func (s *ticketAttachmentS3Store) Probe(ctx context.Context) error {
	if strings.TrimSpace(s.bucket) == "" {
		return fmt.Errorf("ticket attachment S3 bucket is required")
	}
	if _, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.bucket}); err != nil {
		return fmt.Errorf("head ticket attachment S3 bucket: %w", err)
	}
	key := ".probe/" + uuid.NewString()
	payload := []byte("ticket-attachment-storage-probe:" + uuid.NewString())
	if err := s.Put(ctx, key, payload, "text/plain"); err != nil {
		return err
	}
	defer func() { _ = s.Delete(context.Background(), key) }()
	body, err := s.Open(ctx, key)
	if err != nil {
		return err
	}
	read, err := io.ReadAll(body)
	closeErr := body.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if sha256.Sum256(read) != sha256.Sum256(payload) {
		return fmt.Errorf("ticket attachment S3 probe checksum mismatch")
	}
	return s.Delete(ctx, key)
}

func (s *ticketAttachmentS3Store) key(objectKey string) string {
	objectKey = strings.TrimLeft(strings.TrimSpace(objectKey), "/")
	if s.prefix == "" {
		return objectKey
	}
	return s.prefix + "/" + objectKey
}

func domainStorageProviderUnavailable(message string) error {
	return fmt.Errorf("%s: %w", message, domain.ErrTicketStorageProviderUnavailable)
}
