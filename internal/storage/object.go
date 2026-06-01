package storage

import (
	"context"
	"io"
	"time"
)

type ObjectStorageProvider interface {
	PutObject(ctx context.Context, input PutObjectInput) (PutObjectOutput, error)
	HeadObject(ctx context.Context, input HeadObjectInput) (ObjectInfo, error)
	DeleteObject(ctx context.Context, input DeleteObjectInput) error
	PresignGetObject(ctx context.Context, input PresignObjectInput) (PresignedURL, error)
	PresignPutObject(ctx context.Context, input PresignObjectInput) (PresignedURL, error)
	CreateMultipartUpload(ctx context.Context, input MultipartUploadInput) (MultipartUpload, error)
	PresignUploadPart(ctx context.Context, input PresignUploadPartInput) (PresignedURL, error)
	CompleteMultipartUpload(ctx context.Context, input CompleteMultipartUploadInput) (ObjectInfo, error)
	AbortMultipartUpload(ctx context.Context, input AbortMultipartUploadInput) error
}

type PutObjectInput struct {
	Bucket      string
	ObjectKey   string
	Body        io.Reader
	SizeBytes   int64
	ContentType string
	Metadata    map[string]string
}

type PutObjectOutput struct {
	ETag string
}

type HeadObjectInput struct {
	Bucket    string
	ObjectKey string
}

type DeleteObjectInput struct {
	Bucket    string
	ObjectKey string
}

type PresignObjectInput struct {
	Bucket      string
	ObjectKey   string
	ContentType string
	ExpiresIn   time.Duration
	Headers     map[string]string
}

type PresignedURL struct {
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

type ObjectInfo struct {
	Bucket       string
	ObjectKey    string
	ETag         string
	ContentType  string
	SizeBytes    int64
	LastModified time.Time
	Metadata     map[string]string
}

type MultipartUploadInput struct {
	Bucket      string
	ObjectKey   string
	ContentType string
	Metadata    map[string]string
}

type MultipartUpload struct {
	Bucket    string
	ObjectKey string
	UploadID  string
}

type PresignUploadPartInput struct {
	Bucket     string
	ObjectKey  string
	UploadID   string
	PartNumber int
	ExpiresIn  time.Duration
}

type CompletedPart struct {
	PartNumber int
	ETag       string
	SizeBytes  int64
}

type CompleteMultipartUploadInput struct {
	Bucket    string
	ObjectKey string
	UploadID  string
	Parts     []CompletedPart
}

type AbortMultipartUploadInput struct {
	Bucket    string
	ObjectKey string
	UploadID  string
}
