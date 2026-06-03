package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"nlserver/internal/config"
	"nlserver/internal/repository"
	"nlserver/internal/storage"
)

const (
	uploadModeAuto      = "auto"
	uploadModeSingle    = "single"
	uploadModeMultipart = "multipart"

	sessionStatusCreated   = "created"
	sessionStatusCompleted = "completed"
	sessionStatusAborted   = "aborted"

	defaultSingleUploadLimitBytes = 64 * 1024 * 1024
	defaultMultipartPartSizeBytes = 8 * 1024 * 1024
	maxMultipartParts             = 10000

	fileStatusProcessing = "processing"
	fileStatusReady      = "ready"
	fileStatusFailed     = "failed"

	fileProcessingJobImage = "image_metadata"
	fileProcessingJobVideo = "video_cover"
	fileProcessingJobAudio = "audio_metadata"
)

var (
	ErrValidation          = errors.New("files: validation error")
	ErrStorageUnavailable  = errors.New("files: storage unavailable")
	ErrForbidden           = errors.New("files: forbidden")
	ErrUploadExpired       = errors.New("files: upload session expired")
	ErrUploadCompleted     = errors.New("files: upload session completed")
	ErrUploadAborted       = errors.New("files: upload session aborted")
	ErrUploadMode          = errors.New("files: unsupported upload mode")
	ErrFileReferenced      = errors.New("files: file is referenced")
	ErrUploadPartMissing   = errors.New("files: upload part missing")
	ErrUploadPartETag      = errors.New("files: upload part etag mismatch")
	ErrUploadInvalid       = errors.New("files: upload session invalid")
	ErrContentHashMismatch = errors.New("files: content hash mismatch")
)

var targetTypePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Service struct {
	cfg     config.StorageConfig
	repos   repository.Repositories
	db      repository.TxBeginner
	storage storage.ObjectStorageProvider
	scanner MaliciousContentScanner
}

type CreateUploadSessionRequest struct {
	UserID            string
	TargetType        string
	FileName          string
	MimeType          string
	SizeBytes         int64
	ContentHash       *string
	UploadMode        string
	ClientOperationID *string
}

type UploadSessionResult struct {
	UploadSessionID string
	FileID          string
	UploadMode      string
	PartSizeBytes   *int64
	TotalParts      *int
	UploadURL       *string
	Headers         map[string]string
	ExpiresAt       time.Time
}

type UploadSessionStatus struct {
	Status            string
	UploadMode        string
	PartSizeBytes     *int64
	TotalParts        *int
	UploadedParts     []UploadedPart
	ExpiresAt         time.Time
	FileID            string
	TargetType        string
	ExpectedMimeType  *string
	ExpectedSizeBytes *int64
}

type PresignPartsRequest struct {
	UserID      string
	SessionID   string
	PartNumbers []int
}

type PresignedPart struct {
	PartNumber int               `json:"partNumber"`
	UploadURL  string            `json:"uploadUrl"`
	Headers    map[string]string `json:"headers"`
	ExpiresAt  time.Time         `json:"expiresAt"`
}

type CompleteUploadSessionRequest struct {
	UserID      string
	SessionID   string
	ETag        *string
	Parts       []CompletedPart
	ContentHash *string
}

type CompletedPart struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
	SizeBytes  int64  `json:"sizeBytes,omitempty"`
}

type UploadPartError struct {
	Cause              error
	MissingPartNumbers []int
	PartNumbers        []int
}

func (e *UploadPartError) Error() string {
	if e == nil || e.Cause == nil {
		return "files: upload part error"
	}
	return e.Cause.Error()
}

func (e *UploadPartError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type ContentHashMismatchError struct {
	Expected string
	Actual   string
}

func (e *ContentHashMismatchError) Error() string {
	return ErrContentHashMismatch.Error()
}

func (e *ContentHashMismatchError) Unwrap() error {
	return ErrContentHashMismatch
}

type UploadedPart struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
	SizeBytes  int64  `json:"sizeBytes,omitempty"`
}

type CompleteUploadSessionResult struct {
	FileID           string
	URL              string
	ETag             *string
	ContentHash      *string
	Version          int
	ProcessingStatus string
}

type StoreFileRequest struct {
	UserID      string
	TargetType  string
	FileName    string
	MimeType    string
	SizeBytes   int64
	ContentHash *string
	Body        io.Reader
}

type FileVariantResult struct {
	VariantID   string
	VariantType string
	URL         string
	MimeType    string
	SizeBytes   int64
	Width       *int
	Height      *int
	DurationMS  *int
	ETag        *string
	ContentHash *string
	CreatedAt   time.Time
}

type FileResult struct {
	FileID           string
	FileName         string
	MimeType         string
	SizeBytes        int64
	URL              string
	Visibility       string
	ProcessingStatus string
	Version          int
	ETag             *string
	ContentHash      *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Variants         []FileVariantResult
}

type DownloadURLResult struct {
	URL       string
	ExpiresAt time.Time
	Headers   map[string]string
}

type AvatarResult struct {
	FileID            string
	AvatarURL         string
	AvatarVersion     int
	AvatarEtag        *string
	AvatarContentHash *string
}

func NewService(cfg config.StorageConfig, repos repository.Repositories, db repository.TxBeginner, provider storage.ObjectStorageProvider) *Service {
	return &Service{
		cfg:     cfg,
		repos:   repos,
		db:      db,
		storage: provider,
		scanner: noopMaliciousContentScanner{},
	}
}

func (s *Service) SetMaliciousContentScanner(scanner MaliciousContentScanner) {
	if scanner == nil {
		s.scanner = noopMaliciousContentScanner{}
		return
	}
	s.scanner = scanner
}

func (s *Service) StoreFile(ctx context.Context, req StoreFileRequest) (FileResult, error) {
	file, err := s.storeFile(ctx, req)
	if err != nil {
		return FileResult{}, err
	}
	return s.makeFileResult(ctx, file)
}

func (s *Service) UploadAvatar(ctx context.Context, req StoreFileRequest) (AvatarResult, error) {
	req.TargetType = "avatar"
	file, err := s.storeFile(ctx, req)
	if err != nil {
		return AvatarResult{}, err
	}

	var user repository.User
	err = repository.WithTx(ctx, s.db, func(repos repository.Repositories) error {
		if _, err := repos.Files.SoftDeleteUsages(ctx, "avatar", req.UserID); err != nil {
			return err
		}
		if _, err := repos.Files.CreateUsage(ctx, repository.CreateFileUsageParams{
			FileID:      file.ID,
			UsageType:   "avatar",
			BusinessID:  req.UserID,
			OwnerUserID: req.UserID,
			Visibility:  "public",
		}); err != nil {
			return err
		}
		updatedUser, err := repos.Users.UpdateAvatarFile(ctx, req.UserID, file.ID)
		if err != nil {
			return err
		}
		user = updatedUser
		return nil
	})
	if err != nil {
		return AvatarResult{}, err
	}

	signed, err := s.storage.PresignGetObject(ctx, storage.PresignObjectInput{
		Bucket:    file.Bucket,
		ObjectKey: file.ObjectKey,
		ExpiresIn: s.presignTTL(),
	})
	if err != nil {
		return AvatarResult{}, err
	}

	return AvatarResult{
		FileID:            file.ID,
		AvatarURL:         signed.URL,
		AvatarVersion:     user.Version,
		AvatarEtag:        file.ETag,
		AvatarContentHash: file.ContentHash,
	}, nil
}

func (s *Service) GetFile(ctx context.Context, userID string, fileID string) (FileResult, error) {
	if err := s.ensureReady(); err != nil {
		return FileResult{}, err
	}
	file, err := s.repos.Files.Get(ctx, fileID)
	if err != nil {
		return FileResult{}, err
	}
	allowed, err := s.repos.Files.CanUserAccessFile(ctx, userID, file.ID)
	if err != nil {
		return FileResult{}, err
	}
	if !allowed {
		return FileResult{}, ErrForbidden
	}
	return s.makeFileResult(ctx, file)
}

func (s *Service) PresignDownloadURL(ctx context.Context, userID string, fileID string, variantType string) (DownloadURLResult, error) {
	if err := s.ensureReady(); err != nil {
		return DownloadURLResult{}, err
	}
	file, err := s.repos.Files.Get(ctx, fileID)
	if err != nil {
		return DownloadURLResult{}, err
	}
	allowed, err := s.repos.Files.CanUserAccessFile(ctx, userID, file.ID)
	if err != nil {
		return DownloadURLResult{}, err
	}
	if !allowed {
		return DownloadURLResult{}, ErrForbidden
	}

	bucket := file.Bucket
	objectKey := file.ObjectKey
	if strings.TrimSpace(variantType) != "" {
		variants, err := s.repos.Files.ListVariants(ctx, file.ID)
		if err != nil {
			return DownloadURLResult{}, err
		}
		found := false
		for _, variant := range variants {
			if variant.VariantType == strings.TrimSpace(variantType) {
				bucket = variant.Bucket
				objectKey = variant.ObjectKey
				found = true
				break
			}
		}
		if !found {
			return DownloadURLResult{}, repository.ErrNotFound
		}
	}

	signed, err := s.storage.PresignGetObject(ctx, storage.PresignObjectInput{
		Bucket:    bucket,
		ObjectKey: objectKey,
		ExpiresIn: s.presignTTL(),
	})
	if err != nil {
		return DownloadURLResult{}, err
	}
	return DownloadURLResult{URL: signed.URL, ExpiresAt: signed.ExpiresAt, Headers: signed.Headers}, nil
}

func (s *Service) DeleteFile(ctx context.Context, userID string, fileID string) error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	file, err := s.repos.Files.Get(ctx, fileID)
	if err != nil {
		return err
	}
	if file.OwnerUserID == nil || *file.OwnerUserID != userID {
		return ErrForbidden
	}
	referenced, err := s.repos.Files.HasActiveReferences(ctx, file.ID)
	if err != nil {
		return err
	}
	if referenced {
		return ErrFileReferenced
	}
	variants, err := s.repos.Files.ListVariants(ctx, file.ID)
	if err != nil {
		return err
	}

	_ = s.storage.DeleteObject(ctx, storage.DeleteObjectInput{Bucket: file.Bucket, ObjectKey: file.ObjectKey})
	for _, variant := range variants {
		_ = s.storage.DeleteObject(ctx, storage.DeleteObjectInput{Bucket: variant.Bucket, ObjectKey: variant.ObjectKey})
	}
	return s.repos.Files.SoftDelete(ctx, file.ID)
}

func (s *Service) CreateUploadSession(ctx context.Context, req CreateUploadSessionRequest) (UploadSessionResult, error) {
	if err := s.ensureReady(); err != nil {
		return UploadSessionResult{}, err
	}
	if err := validateCreateUploadSession(req); err != nil {
		return UploadSessionResult{}, err
	}
	canonicalTarget, normalizedMime, err := s.validateGovernance(ctx, req.UserID, req.TargetType, req.FileName, req.MimeType, req.SizeBytes)
	if err != nil {
		return UploadSessionResult{}, err
	}
	req.TargetType = canonicalTarget
	req.MimeType = normalizedMime

	clientOperationID := normalizeOptional(req.ClientOperationID)
	if clientOperationID != nil {
		existing, err := s.repos.Files.GetUploadSessionByClientOperation(ctx, req.UserID, *clientOperationID)
		if err == nil {
			file, fileErr := s.repos.Files.Get(ctx, existing.FileID)
			if fileErr != nil {
				return UploadSessionResult{}, fileErr
			}
			return s.makeUploadSessionResult(ctx, existing, file)
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return UploadSessionResult{}, err
		}
	}

	fileID, err := newUUIDString()
	if err != nil {
		return UploadSessionResult{}, err
	}
	sessionID, err := newUUIDString()
	if err != nil {
		return UploadSessionResult{}, err
	}

	mode := chooseUploadMode(req.UploadMode, req.SizeBytes)
	if mode == "" {
		return UploadSessionResult{}, ErrUploadMode
	}

	bucket, visibility := s.bucketForTarget(req.TargetType)
	safeName := safeFileName(req.FileName)
	objectKey := fmt.Sprintf("uploads/%s/%s/%s/%s", normalizePathSegment(req.TargetType), req.UserID, fileID, safeName)
	expiresAt := time.Now().Add(s.presignTTL())

	var (
		s3UploadID    *string
		partSizeBytes *int64
		totalParts    *int
	)
	if mode == uploadModeMultipart {
		partSize := multipartPartSize(req.SizeBytes)
		parts := int(math.Ceil(float64(req.SizeBytes) / float64(partSize)))
		if parts < 1 || parts > maxMultipartParts {
			return UploadSessionResult{}, ErrValidation
		}

		upload, err := s.storage.CreateMultipartUpload(ctx, storage.MultipartUploadInput{
			Bucket:      bucket,
			ObjectKey:   objectKey,
			ContentType: normalizeText(req.MimeType),
			Metadata:    metadataForUpload(req.ContentHash),
		})
		if err != nil {
			return UploadSessionResult{}, err
		}

		s3UploadID = &upload.UploadID
		partSizeBytes = &partSize
		totalParts = &parts
	}

	var (
		file    repository.File
		session repository.FileUploadSession
	)
	err = repository.WithTx(ctx, s.db, func(repos repository.Repositories) error {
		createdFile, err := repos.Files.Create(ctx, repository.CreateFileParams{
			ID:               fileID,
			OwnerUserID:      req.UserID,
			OriginalFileName: req.FileName,
			Bucket:           bucket,
			ObjectKey:        objectKey,
			MimeType:         normalizeText(req.MimeType),
			SizeBytes:        req.SizeBytes,
			ContentHash:      normalizeOptional(req.ContentHash),
			Visibility:       visibility,
		})
		if err != nil {
			return err
		}

		createdSession, err := repos.Files.CreateUploadSession(ctx, repository.CreateUploadSessionParams{
			ID:                sessionID,
			UserID:            req.UserID,
			FileID:            createdFile.ID,
			TargetType:        normalizeText(req.TargetType),
			Status:            sessionStatusCreated,
			UploadMode:        mode,
			S3UploadID:        s3UploadID,
			PartSizeBytes:     partSizeBytes,
			TotalParts:        totalParts,
			UploadedParts:     json.RawMessage(`[]`),
			ExpectedMimeType:  normalizeText(req.MimeType),
			ExpectedSizeBytes: req.SizeBytes,
			ExpiresAt:         expiresAt,
			ClientOperationID: clientOperationID,
		})
		if err != nil {
			return err
		}

		file = createdFile
		session = createdSession
		return nil
	})
	if err != nil {
		if mode == uploadModeMultipart && s3UploadID != nil {
			_ = s.storage.AbortMultipartUpload(ctx, storage.AbortMultipartUploadInput{
				Bucket:    bucket,
				ObjectKey: objectKey,
				UploadID:  *s3UploadID,
			})
		}
		return UploadSessionResult{}, err
	}

	return s.makeUploadSessionResult(ctx, session, file)
}

func (s *Service) GetUploadSession(ctx context.Context, userID string, sessionID string) (UploadSessionStatus, error) {
	if s.repos.Files == nil {
		return UploadSessionStatus{}, ErrStorageUnavailable
	}

	session, err := s.repos.Files.GetUploadSession(ctx, userID, sessionID)
	if err != nil {
		return UploadSessionStatus{}, err
	}

	return makeUploadSessionStatus(session)
}

func (s *Service) PresignUploadParts(ctx context.Context, req PresignPartsRequest) ([]PresignedPart, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if len(req.PartNumbers) == 0 {
		return nil, ErrValidation
	}

	session, file, err := s.getMutableSessionAndFile(ctx, req.UserID, req.SessionID)
	if err != nil {
		return nil, err
	}
	if session.UploadMode != uploadModeMultipart || session.S3UploadID == nil || session.TotalParts == nil {
		return nil, ErrUploadMode
	}

	parts := make([]PresignedPart, 0, len(req.PartNumbers))
	for _, partNumber := range req.PartNumbers {
		if partNumber < 1 || partNumber > *session.TotalParts {
			return nil, ErrValidation
		}

		signed, err := s.storage.PresignUploadPart(ctx, storage.PresignUploadPartInput{
			Bucket:     file.Bucket,
			ObjectKey:  file.ObjectKey,
			UploadID:   *session.S3UploadID,
			PartNumber: partNumber,
			ExpiresIn:  s.presignTTL(),
		})
		if err != nil {
			return nil, err
		}

		parts = append(parts, PresignedPart{
			PartNumber: partNumber,
			UploadURL:  signed.URL,
			Headers:    signed.Headers,
			ExpiresAt:  signed.ExpiresAt,
		})
	}

	return parts, nil
}

func (s *Service) CompleteUploadSession(ctx context.Context, req CompleteUploadSessionRequest) (CompleteUploadSessionResult, error) {
	if err := s.ensureReady(); err != nil {
		return CompleteUploadSessionResult{}, err
	}

	session, file, err := s.getMutableSessionAndFile(ctx, req.UserID, req.SessionID)
	if err != nil {
		return CompleteUploadSessionResult{}, err
	}

	var objectInfo storage.ObjectInfo
	uploadedParts := json.RawMessage(`[]`)
	switch session.UploadMode {
	case uploadModeSingle:
		objectInfo, err = s.storage.HeadObject(ctx, storage.HeadObjectInput{
			Bucket:    file.Bucket,
			ObjectKey: file.ObjectKey,
		})
		if err != nil {
			return CompleteUploadSessionResult{}, err
		}
	case uploadModeMultipart:
		if session.S3UploadID == nil {
			return CompleteUploadSessionResult{}, ErrUploadMode
		}

		normalizedParts, err := validateCompletedMultipartParts(session, req.Parts)
		if err != nil {
			return CompleteUploadSessionResult{}, err
		}

		storageParts := make([]storage.CompletedPart, 0, len(normalizedParts))
		for _, part := range normalizedParts {
			storageParts = append(storageParts, storage.CompletedPart(part))
		}
		objectInfo, err = s.storage.CompleteMultipartUpload(ctx, storage.CompleteMultipartUploadInput{
			Bucket:    file.Bucket,
			ObjectKey: file.ObjectKey,
			UploadID:  *session.S3UploadID,
			Parts:     storageParts,
		})
		if err != nil {
			return CompleteUploadSessionResult{}, mapMultipartCompletionError(err)
		}

		uploadedPartValues := make([]UploadedPart, 0, len(normalizedParts))
		for _, part := range normalizedParts {
			uploadedPartValues = append(uploadedPartValues, UploadedPart(part))
		}
		uploadedParts, _ = json.Marshal(uploadedPartValues)
	default:
		return CompleteUploadSessionResult{}, ErrUploadMode
	}
	if err := validateCompletedObject(session, file, objectInfo); err != nil {
		return CompleteUploadSessionResult{}, err
	}

	contentHash := normalizeOptional(req.ContentHash)
	if contentHash == nil {
		contentHash = file.ContentHash
	}
	if contentHash != nil {
		if err := s.verifyObjectContentHash(ctx, file, *contentHash); err != nil {
			return CompleteUploadSessionResult{}, err
		}
	}
	etag := normalizeOptional(&objectInfo.ETag)

	updatedFile, err := s.finalizeUploadedFile(ctx, file.ID, session.TargetType, etag, contentHash, objectInfo.SizeBytes, func(repos repository.Repositories) error {
		_, err := repos.Files.CompleteUploadSession(ctx, repository.CompleteUploadSessionParams{
			SessionID:     session.ID,
			UserID:        req.UserID,
			UploadedParts: uploadedParts,
		})
		return err
	})
	if err != nil {
		return CompleteUploadSessionResult{}, err
	}

	downloadURL, err := s.storage.PresignGetObject(ctx, storage.PresignObjectInput{
		Bucket:    updatedFile.Bucket,
		ObjectKey: updatedFile.ObjectKey,
		ExpiresIn: s.presignTTL(),
	})
	if err != nil {
		return CompleteUploadSessionResult{}, err
	}

	return CompleteUploadSessionResult{
		FileID:           updatedFile.ID,
		URL:              downloadURL.URL,
		ETag:             updatedFile.ETag,
		ContentHash:      updatedFile.ContentHash,
		Version:          updatedFile.Version,
		ProcessingStatus: updatedFile.StorageStatus,
	}, nil
}

func (s *Service) AbortUploadSession(ctx context.Context, userID string, sessionID string) error {
	if err := s.ensureReady(); err != nil {
		return err
	}

	session, file, err := s.getMutableSessionAndFile(ctx, userID, sessionID)
	if err != nil {
		return err
	}

	if session.UploadMode == uploadModeMultipart && session.S3UploadID != nil {
		if err := s.storage.AbortMultipartUpload(ctx, storage.AbortMultipartUploadInput{
			Bucket:    file.Bucket,
			ObjectKey: file.ObjectKey,
			UploadID:  *session.S3UploadID,
		}); err != nil {
			return err
		}
	} else {
		_ = s.storage.DeleteObject(ctx, storage.DeleteObjectInput{
			Bucket:    file.Bucket,
			ObjectKey: file.ObjectKey,
		})
	}

	return repository.WithTx(ctx, s.db, func(repos repository.Repositories) error {
		if _, err := repos.Files.MarkUploadSessionStatus(ctx, userID, sessionID, sessionStatusAborted); err != nil {
			return err
		}
		_, err := repos.Files.MarkStatus(ctx, file.ID, "aborted")
		return err
	})
}

func (s *Service) ensureReady() error {
	if s.repos.Files == nil || s.db == nil || s.storage == nil {
		return ErrStorageUnavailable
	}
	return nil
}

func (s *Service) storeFile(ctx context.Context, req StoreFileRequest) (repository.File, error) {
	if err := s.ensureReady(); err != nil {
		return repository.File{}, err
	}
	if err := validateStoreFile(req); err != nil {
		return repository.File{}, err
	}
	prepared, err := s.prepareProxyUpload(ctx, req)
	if err != nil {
		return repository.File{}, err
	}
	req = prepared

	fileID, err := newUUIDString()
	if err != nil {
		return repository.File{}, err
	}
	bucket, visibility := s.bucketForTarget(req.TargetType)
	safeName := safeFileName(req.FileName)
	objectKey := fmt.Sprintf("uploads/%s/%s/%s/%s", normalizePathSegment(req.TargetType), req.UserID, fileID, safeName)

	var file repository.File
	err = repository.WithTx(ctx, s.db, func(repos repository.Repositories) error {
		createdFile, err := repos.Files.Create(ctx, repository.CreateFileParams{
			ID:               fileID,
			OwnerUserID:      req.UserID,
			OriginalFileName: req.FileName,
			Bucket:           bucket,
			ObjectKey:        objectKey,
			MimeType:         normalizeText(req.MimeType),
			SizeBytes:        req.SizeBytes,
			ContentHash:      normalizeOptional(req.ContentHash),
			Visibility:       visibility,
		})
		if err != nil {
			return err
		}
		file = createdFile
		return nil
	})
	if err != nil {
		return repository.File{}, err
	}

	put, err := s.storage.PutObject(ctx, storage.PutObjectInput{
		Bucket:      file.Bucket,
		ObjectKey:   file.ObjectKey,
		Body:        req.Body,
		SizeBytes:   req.SizeBytes,
		ContentType: file.MimeType,
		Metadata:    metadataForUpload(req.ContentHash),
	})
	if err != nil {
		return repository.File{}, err
	}

	etag := normalizeOptional(&put.ETag)
	return s.finalizeUploadedFile(ctx, file.ID, req.TargetType, etag, normalizeOptional(req.ContentHash), req.SizeBytes, nil)
}

func (s *Service) finalizeUploadedFile(ctx context.Context, fileID string, targetType string, etag *string, contentHash *string, sizeBytes int64, extra func(repository.Repositories) error) (repository.File, error) {
	file, err := s.repos.Files.Get(ctx, fileID)
	if err != nil {
		return repository.File{}, err
	}
	scanned := file
	scanned.ETag = etag
	scanned.ContentHash = contentHash
	if sizeBytes > 0 {
		scanned.SizeBytes = sizeBytes
	}
	if err := s.scanUploadedObject(ctx, scanned, targetType); err != nil {
		return repository.File{}, err
	}

	var completedFile repository.File
	err = repository.WithTx(ctx, s.db, func(repos repository.Repositories) error {
		updatedFile, err := repos.Files.MarkUploaded(ctx, fileID, etag, contentHash, sizeBytes)
		if err != nil {
			return err
		}
		if extra != nil {
			if err := extra(repos); err != nil {
				return err
			}
		}

		jobType := processingJobTypeForFile(updatedFile)
		if jobType != "" {
			if _, err := repos.Files.CreateProcessingJob(ctx, repository.CreateFileProcessingJobParams{
				FileID:  updatedFile.ID,
				JobType: jobType,
				Status:  "pending",
			}); err != nil {
				return err
			}
			updatedFile, err = repos.Files.MarkStatus(ctx, updatedFile.ID, fileStatusProcessing)
			if err != nil {
				return err
			}
		} else {
			updatedFile, err = repos.Files.MarkStatus(ctx, updatedFile.ID, fileStatusReady)
			if err != nil {
				return err
			}
		}

		completedFile = updatedFile
		return nil
	})
	if err != nil {
		return repository.File{}, err
	}
	return completedFile, nil
}

func (s *Service) makeFileResult(ctx context.Context, file repository.File) (FileResult, error) {
	signed, err := s.storage.PresignGetObject(ctx, storage.PresignObjectInput{
		Bucket:    file.Bucket,
		ObjectKey: file.ObjectKey,
		ExpiresIn: s.presignTTL(),
	})
	if err != nil {
		return FileResult{}, err
	}

	variants, err := s.repos.Files.ListVariants(ctx, file.ID)
	if err != nil {
		return FileResult{}, err
	}

	variantResults := make([]FileVariantResult, 0, len(variants))
	for _, variant := range variants {
		variantURL, err := s.storage.PresignGetObject(ctx, storage.PresignObjectInput{
			Bucket:    variant.Bucket,
			ObjectKey: variant.ObjectKey,
			ExpiresIn: s.presignTTL(),
		})
		if err != nil {
			return FileResult{}, err
		}
		variantResults = append(variantResults, FileVariantResult{
			VariantID:   variant.ID,
			VariantType: variant.VariantType,
			URL:         variantURL.URL,
			MimeType:    variant.MimeType,
			SizeBytes:   variant.SizeBytes,
			Width:       variant.Width,
			Height:      variant.Height,
			DurationMS:  variant.DurationMS,
			ETag:        variant.ETag,
			ContentHash: variant.ContentHash,
			CreatedAt:   variant.CreatedAt,
		})
	}

	return FileResult{
		FileID:           file.ID,
		FileName:         file.OriginalFileName,
		MimeType:         file.MimeType,
		SizeBytes:        file.SizeBytes,
		URL:              signed.URL,
		Visibility:       file.Visibility,
		ProcessingStatus: file.StorageStatus,
		Version:          file.Version,
		ETag:             file.ETag,
		ContentHash:      file.ContentHash,
		CreatedAt:        file.CreatedAt,
		UpdatedAt:        file.UpdatedAt,
		Variants:         variantResults,
	}, nil
}

func (s *Service) makeUploadSessionResult(ctx context.Context, session repository.FileUploadSession, file repository.File) (UploadSessionResult, error) {
	result := UploadSessionResult{
		UploadSessionID: session.ID,
		FileID:          file.ID,
		UploadMode:      session.UploadMode,
		PartSizeBytes:   session.PartSizeBytes,
		TotalParts:      session.TotalParts,
		Headers:         map[string]string{},
		ExpiresAt:       session.ExpiresAt,
	}

	if session.UploadMode == uploadModeSingle {
		signed, err := s.storage.PresignPutObject(ctx, storage.PresignObjectInput{
			Bucket:      file.Bucket,
			ObjectKey:   file.ObjectKey,
			ContentType: file.MimeType,
			ExpiresIn:   s.presignTTL(),
		})
		if err != nil {
			return UploadSessionResult{}, err
		}
		result.UploadURL = &signed.URL
		result.Headers = signed.Headers
		result.ExpiresAt = signed.ExpiresAt
	}

	return result, nil
}

func (s *Service) getMutableSessionAndFile(ctx context.Context, userID string, sessionID string) (repository.FileUploadSession, repository.File, error) {
	session, err := s.repos.Files.GetUploadSession(ctx, userID, sessionID)
	if err != nil {
		return repository.FileUploadSession{}, repository.File{}, err
	}
	switch session.Status {
	case sessionStatusCompleted:
		return repository.FileUploadSession{}, repository.File{}, ErrUploadCompleted
	case sessionStatusAborted:
		return repository.FileUploadSession{}, repository.File{}, ErrUploadAborted
	}
	if time.Now().After(session.ExpiresAt) {
		return repository.FileUploadSession{}, repository.File{}, ErrUploadExpired
	}

	file, err := s.repos.Files.Get(ctx, session.FileID)
	if err != nil {
		return repository.FileUploadSession{}, repository.File{}, err
	}
	if file.OwnerUserID == nil || *file.OwnerUserID != userID {
		return repository.FileUploadSession{}, repository.File{}, ErrForbidden
	}

	return session, file, nil
}

func makeUploadSessionStatus(session repository.FileUploadSession) (UploadSessionStatus, error) {
	var uploadedParts []UploadedPart
	if len(session.UploadedParts) > 0 {
		if err := json.Unmarshal(session.UploadedParts, &uploadedParts); err != nil {
			return UploadSessionStatus{}, err
		}
	}

	return UploadSessionStatus{
		Status:            session.Status,
		UploadMode:        session.UploadMode,
		PartSizeBytes:     session.PartSizeBytes,
		TotalParts:        session.TotalParts,
		UploadedParts:     uploadedParts,
		ExpiresAt:         session.ExpiresAt,
		FileID:            session.FileID,
		TargetType:        session.TargetType,
		ExpectedMimeType:  session.ExpectedMimeType,
		ExpectedSizeBytes: session.ExpectedSizeBytes,
	}, nil
}

func validateCreateUploadSession(req CreateUploadSessionRequest) error {
	if strings.TrimSpace(req.UserID) == "" ||
		strings.TrimSpace(req.TargetType) == "" ||
		strings.TrimSpace(req.FileName) == "" ||
		strings.TrimSpace(req.MimeType) == "" ||
		req.SizeBytes <= 0 {
		return ErrValidation
	}
	if !targetTypePattern.MatchString(strings.TrimSpace(req.TargetType)) {
		return ErrValidation
	}
	switch normalizeUploadMode(req.UploadMode) {
	case "", uploadModeAuto, uploadModeSingle, uploadModeMultipart:
		return nil
	default:
		return ErrUploadMode
	}
}

func validateStoreFile(req StoreFileRequest) error {
	if strings.TrimSpace(req.UserID) == "" ||
		strings.TrimSpace(req.TargetType) == "" ||
		strings.TrimSpace(req.FileName) == "" ||
		strings.TrimSpace(req.MimeType) == "" ||
		req.SizeBytes <= 0 ||
		req.Body == nil {
		return ErrValidation
	}
	if !targetTypePattern.MatchString(strings.TrimSpace(req.TargetType)) {
		return ErrValidation
	}
	return nil
}

func chooseUploadMode(mode string, sizeBytes int64) string {
	mode = normalizeUploadMode(mode)
	if mode == "" || mode == uploadModeAuto {
		if sizeBytes <= defaultSingleUploadLimitBytes {
			return uploadModeSingle
		}
		return uploadModeMultipart
	}
	if mode == uploadModeSingle || mode == uploadModeMultipart {
		return mode
	}
	return ""
}

func (s *Service) bucketForTarget(targetType string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(targetType)) {
	case "avatar", "user_avatar":
		return s.cfg.Buckets.Public, "public"
	case "group_avatar", "chat_attachment", "chat_image", "chat_video", "chat_audio", "chat_file", "post_image", "post_video", "post_live", "post_media":
		return s.cfg.Buckets.Protected, "protected"
	default:
		return s.cfg.Buckets.Private, "private"
	}
}

func (s *Service) presignTTL() time.Duration {
	if s.cfg.PresignTTL > 0 {
		return s.cfg.PresignTTL
	}
	return 10 * time.Minute
}

func multipartPartSize(sizeBytes int64) int64 {
	partSize := int64(defaultMultipartPartSizeBytes)
	minPartSize := int64(math.Ceil(float64(sizeBytes) / float64(maxMultipartParts)))
	if minPartSize > partSize {
		partSize = minPartSize
	}
	remainder := partSize % (1024 * 1024)
	if remainder != 0 {
		partSize += (1024 * 1024) - remainder
	}
	return partSize
}

func metadataForUpload(contentHash *string) map[string]string {
	metadata := map[string]string{}
	if value := normalizeOptional(contentHash); value != nil {
		metadata["content-hash"] = *value
	}
	return metadata
}

func normalizeUploadMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

func processingJobTypeForFile(file repository.File) string {
	mimeType := strings.ToLower(strings.TrimSpace(file.MimeType))
	if strings.HasPrefix(mimeType, "image/") {
		return fileProcessingJobImage
	}
	if strings.HasPrefix(mimeType, "video/") {
		return fileProcessingJobVideo
	}
	if strings.HasPrefix(mimeType, "audio/") {
		return fileProcessingJobAudio
	}
	return ""
}

func (s *Service) prepareProxyUpload(ctx context.Context, req StoreFileRequest) (StoreFileRequest, error) {
	var prefix bytes.Buffer
	limited := io.LimitReader(req.Body, 512)
	if _, err := io.Copy(&prefix, limited); err != nil {
		return StoreFileRequest{}, err
	}
	body := io.MultiReader(bytes.NewReader(prefix.Bytes()), req.Body)
	mimeType := sniffedMimeType(prefix.Bytes(), req.MimeType)
	canonicalTarget, normalizedMime, err := s.validateGovernance(ctx, req.UserID, req.TargetType, req.FileName, mimeType, req.SizeBytes)
	if err != nil {
		return StoreFileRequest{}, err
	}
	req.TargetType = canonicalTarget
	req.MimeType = normalizedMime
	req.Body = body
	return req, nil
}

func validateCompletedObject(session repository.FileUploadSession, file repository.File, objectInfo storage.ObjectInfo) error {
	expectedSize := file.SizeBytes
	if session.ExpectedSizeBytes != nil {
		expectedSize = *session.ExpectedSizeBytes
	}
	if objectInfo.SizeBytes != expectedSize {
		if objectInfo.SizeBytes > expectedSize {
			return ErrFileTooLarge
		}
		return ErrValidation
	}
	mimeType := objectInfo.ContentType
	if strings.TrimSpace(mimeType) == "" {
		mimeType = file.MimeType
	}
	_, _, err := validateTargetPolicy(session.TargetType, file.OriginalFileName, mimeType, objectInfo.SizeBytes)
	return err
}

func validateCompletedMultipartParts(session repository.FileUploadSession, parts []CompletedPart) ([]CompletedPart, error) {
	if len(parts) == 0 || session.TotalParts == nil || *session.TotalParts <= 0 {
		return nil, ErrValidation
	}

	totalParts := *session.TotalParts
	byNumber := make(map[int]CompletedPart, len(parts))
	duplicatePartNumbers := []int{}
	for _, part := range parts {
		part.ETag = normalizeETagValue(part.ETag)
		if part.PartNumber < 1 || part.PartNumber > totalParts || part.ETag == "" || part.SizeBytes < 0 {
			return nil, ErrValidation
		}
		if existing, ok := byNumber[part.PartNumber]; ok {
			if existing.ETag != part.ETag || existing.SizeBytes != part.SizeBytes {
				duplicatePartNumbers = append(duplicatePartNumbers, part.PartNumber)
			}
			continue
		}
		byNumber[part.PartNumber] = part
	}
	if len(duplicatePartNumbers) > 0 {
		sort.Ints(duplicatePartNumbers)
		return nil, &UploadPartError{Cause: ErrUploadPartETag, PartNumbers: compactInts(duplicatePartNumbers)}
	}

	missingPartNumbers := []int{}
	normalized := make([]CompletedPart, 0, totalParts)
	for partNumber := 1; partNumber <= totalParts; partNumber++ {
		part, ok := byNumber[partNumber]
		if !ok {
			missingPartNumbers = append(missingPartNumbers, partNumber)
			continue
		}
		if err := validateCompletedPartSize(session, part); err != nil {
			return nil, err
		}
		normalized = append(normalized, part)
	}
	if len(missingPartNumbers) > 0 {
		return nil, &UploadPartError{Cause: ErrUploadPartMissing, MissingPartNumbers: missingPartNumbers}
	}

	return normalized, nil
}

func validateCompletedPartSize(session repository.FileUploadSession, part CompletedPart) error {
	if part.SizeBytes == 0 || session.PartSizeBytes == nil || session.TotalParts == nil || session.ExpectedSizeBytes == nil {
		return nil
	}
	expectedSize := *session.PartSizeBytes
	if part.PartNumber == *session.TotalParts {
		previousPartBytes := int64(*session.TotalParts-1) * *session.PartSizeBytes
		expectedSize = *session.ExpectedSizeBytes - previousPartBytes
	}
	if expectedSize <= 0 || part.SizeBytes != expectedSize {
		return ErrValidation
	}
	return nil
}

func (s *Service) verifyObjectContentHash(ctx context.Context, file repository.File, expectedHash string) error {
	expected, ok := normalizeSHA256Hash(expectedHash)
	if !ok {
		return ErrValidation
	}

	object, err := s.storage.GetObject(ctx, storage.GetObjectInput{
		Bucket:    file.Bucket,
		ObjectKey: file.ObjectKey,
	})
	if err != nil {
		return err
	}
	defer object.Body.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, object.Body); err != nil {
		return err
	}
	actual := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if actual != expected {
		return &ContentHashMismatchError{
			Expected: expected,
			Actual:   actual,
		}
	}
	return nil
}

func mapMultipartCompletionError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "nosuchupload"), strings.Contains(message, "no such upload"), strings.Contains(message, "upload does not exist"):
		return fmt.Errorf("%w: %v", ErrUploadInvalid, err)
	case strings.Contains(message, "invalidpart"), strings.Contains(message, "etag"), strings.Contains(message, "part"):
		return &UploadPartError{Cause: ErrUploadPartETag}
	default:
		return err
	}
}

func normalizeSHA256Hash(value string) (string, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	trimmed = strings.TrimPrefix(trimmed, "sha256:")
	if len(trimmed) != sha256.Size*2 {
		return "", false
	}
	for _, r := range trimmed {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", false
		}
	}
	return "sha256:" + trimmed, true
}

func normalizeETagValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"`)
}

func compactInts(values []int) []int {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func normalizeText(value string) string {
	return strings.TrimSpace(value)
}

func normalizeOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizePathSegment(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	if normalized == "" {
		return "file"
	}
	return normalized
}

func safeFileName(value string) string {
	name := filepath.Base(strings.TrimSpace(value))
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "upload.bin"
	}
	return name
}

func newUUIDString() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}

	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	encoded := hex.EncodeToString(bytes[:])
	return encoded[0:8] + "-" +
		encoded[8:12] + "-" +
		encoded[12:16] + "-" +
		encoded[16:20] + "-" +
		encoded[20:32], nil
}
