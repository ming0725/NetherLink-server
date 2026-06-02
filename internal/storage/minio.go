package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"nlserver/internal/config"
)

type minioProvider struct {
	client        *minio.Client
	presignClient *minio.Client
	core          *minio.Core
}

func newMinIOProvider(cfg config.StorageConfig) (ObjectStorageProvider, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("storage: MinIO endpoint is required")
	}

	internalEndpoint := strings.TrimSpace(cfg.InternalEndpoint)
	if internalEndpoint == "" {
		internalEndpoint = endpoint
	}

	client, err := newMinIOClient(internalEndpoint, cfg)
	if err != nil {
		return nil, err
	}

	presignClient := client
	if internalEndpoint != endpoint {
		presignClient, err = newMinIOClient(endpoint, cfg)
		if err != nil {
			return nil, err
		}
	}

	core := &minio.Core{Client: client}
	return &minioProvider{
		client:        client,
		presignClient: presignClient,
		core:          core,
	}, nil
}

func newMinIOClient(endpoint string, cfg config.StorageConfig) (*minio.Client, error) {
	parsed, err := parseMinIOEndpoint(endpoint, cfg.UseSSL)
	if err != nil {
		return nil, err
	}

	return minio.New(parsed.host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: parsed.secure,
	})
}

type parsedEndpoint struct {
	host   string
	secure bool
}

func parseMinIOEndpoint(endpoint string, fallbackSecure bool) (parsedEndpoint, error) {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return parsedEndpoint{}, fmt.Errorf("storage: MinIO endpoint is required")
	}

	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return parsedEndpoint{}, err
		}
		if parsed.Host == "" {
			return parsedEndpoint{}, fmt.Errorf("storage: invalid MinIO endpoint %q", endpoint)
		}

		return parsedEndpoint{
			host:   parsed.Host,
			secure: strings.EqualFold(parsed.Scheme, "https"),
		}, nil
	}

	return parsedEndpoint{host: trimmed, secure: fallbackSecure}, nil
}

func (p *minioProvider) PutObject(ctx context.Context, input PutObjectInput) (PutObjectOutput, error) {
	info, err := p.client.PutObject(ctx, input.Bucket, input.ObjectKey, input.Body, input.SizeBytes, minio.PutObjectOptions{
		ContentType:  input.ContentType,
		UserMetadata: input.Metadata,
	})
	if err != nil {
		return PutObjectOutput{}, err
	}

	return PutObjectOutput{ETag: normalizeETag(info.ETag)}, nil
}

func (p *minioProvider) GetObject(ctx context.Context, input GetObjectInput) (ObjectReader, error) {
	object, err := p.client.GetObject(ctx, input.Bucket, input.ObjectKey, minio.GetObjectOptions{})
	if err != nil {
		return ObjectReader{}, err
	}

	info, err := object.Stat()
	if err != nil {
		_ = object.Close()
		return ObjectReader{}, err
	}

	return ObjectReader{
		Body: object,
		Info: makeObjectInfo(input.Bucket, input.ObjectKey, info),
	}, nil
}

func (p *minioProvider) HeadObject(ctx context.Context, input HeadObjectInput) (ObjectInfo, error) {
	info, err := p.client.StatObject(ctx, input.Bucket, input.ObjectKey, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, err
	}

	return makeObjectInfo(input.Bucket, input.ObjectKey, info), nil
}

func (p *minioProvider) DeleteObject(ctx context.Context, input DeleteObjectInput) error {
	return p.client.RemoveObject(ctx, input.Bucket, input.ObjectKey, minio.RemoveObjectOptions{})
}

func (p *minioProvider) PresignGetObject(ctx context.Context, input PresignObjectInput) (PresignedURL, error) {
	expiresIn := normalizeExpiry(input.ExpiresIn)
	signedURL, err := p.presignClient.PresignedGetObject(ctx, input.Bucket, input.ObjectKey, expiresIn, nil)
	if err != nil {
		return PresignedURL{}, err
	}

	return PresignedURL{
		URL:       signedURL.String(),
		Headers:   map[string]string{},
		ExpiresAt: time.Now().Add(expiresIn),
	}, nil
}

func (p *minioProvider) PresignPutObject(ctx context.Context, input PresignObjectInput) (PresignedURL, error) {
	expiresIn := normalizeExpiry(input.ExpiresIn)
	headers := signedHeaders(input.ContentType, input.Headers)

	var (
		signedURL *url.URL
		err       error
	)
	if len(headers) == 0 {
		signedURL, err = p.presignClient.PresignedPutObject(ctx, input.Bucket, input.ObjectKey, expiresIn)
	} else {
		signedURL, err = p.presignClient.PresignHeader(ctx, http.MethodPut, input.Bucket, input.ObjectKey, expiresIn, nil, headers)
	}
	if err != nil {
		return PresignedURL{}, err
	}

	return PresignedURL{
		URL:       signedURL.String(),
		Headers:   flattenHeaders(headers),
		ExpiresAt: time.Now().Add(expiresIn),
	}, nil
}

func (p *minioProvider) CreateMultipartUpload(ctx context.Context, input MultipartUploadInput) (MultipartUpload, error) {
	uploadID, err := p.core.NewMultipartUpload(ctx, input.Bucket, input.ObjectKey, minio.PutObjectOptions{
		ContentType:  input.ContentType,
		UserMetadata: input.Metadata,
	})
	if err != nil {
		return MultipartUpload{}, err
	}

	return MultipartUpload{
		Bucket:    input.Bucket,
		ObjectKey: input.ObjectKey,
		UploadID:  uploadID,
	}, nil
}

func (p *minioProvider) PresignUploadPart(ctx context.Context, input PresignUploadPartInput) (PresignedURL, error) {
	expiresIn := normalizeExpiry(input.ExpiresIn)
	values := url.Values{}
	values.Set("uploadId", input.UploadID)
	values.Set("partNumber", fmt.Sprintf("%d", input.PartNumber))

	signedURL, err := p.presignClient.Presign(ctx, http.MethodPut, input.Bucket, input.ObjectKey, expiresIn, values)
	if err != nil {
		return PresignedURL{}, err
	}

	return PresignedURL{
		URL:       signedURL.String(),
		Headers:   map[string]string{},
		ExpiresAt: time.Now().Add(expiresIn),
	}, nil
}

func (p *minioProvider) CompleteMultipartUpload(ctx context.Context, input CompleteMultipartUploadInput) (ObjectInfo, error) {
	parts := make([]minio.CompletePart, 0, len(input.Parts))
	for _, part := range input.Parts {
		parts = append(parts, minio.CompletePart{
			PartNumber: part.PartNumber,
			ETag:       normalizeETag(part.ETag),
		})
	}

	if _, err := p.core.CompleteMultipartUpload(ctx, input.Bucket, input.ObjectKey, input.UploadID, parts, minio.PutObjectOptions{}); err != nil {
		return ObjectInfo{}, err
	}

	return p.HeadObject(ctx, HeadObjectInput{
		Bucket:    input.Bucket,
		ObjectKey: input.ObjectKey,
	})
}

func (p *minioProvider) AbortMultipartUpload(ctx context.Context, input AbortMultipartUploadInput) error {
	return p.core.AbortMultipartUpload(ctx, input.Bucket, input.ObjectKey, input.UploadID)
}

func makeObjectInfo(bucket string, objectKey string, info minio.ObjectInfo) ObjectInfo {
	metadata := map[string]string{}
	for key, value := range info.UserMetadata {
		metadata[key] = value
	}

	return ObjectInfo{
		Bucket:       bucket,
		ObjectKey:    objectKey,
		ETag:         normalizeETag(info.ETag),
		ContentType:  info.ContentType,
		SizeBytes:    info.Size,
		LastModified: info.LastModified,
		Metadata:     metadata,
	}
}

func signedHeaders(contentType string, headers map[string]string) http.Header {
	out := http.Header{}
	if strings.TrimSpace(contentType) != "" {
		out.Set("Content-Type", strings.TrimSpace(contentType))
	}
	for key, value := range headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out.Set(key, value)
		}
	}
	return out
}

func flattenHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			out[key] = values[0]
		}
	}
	return out
}

func normalizeExpiry(expiresIn time.Duration) time.Duration {
	if expiresIn <= 0 {
		return 10 * time.Minute
	}
	return expiresIn
}

func normalizeETag(etag string) string {
	return strings.Trim(strings.TrimSpace(etag), `"`)
}
