package files

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"nlserver/internal/repository"
)

const (
	kib = 1024
	mib = 1024 * kib
)

var (
	ErrFileTooLarge       = errors.New("files: file too large")
	ErrFileTypeNotAllowed = errors.New("files: file type not allowed")
	ErrFileQuotaExceeded  = errors.New("files: file quota exceeded")
	ErrFileScanRejected   = errors.New("files: file scan rejected")
)

type targetGovernancePolicy struct {
	TargetType   string
	MaxBytes     int64
	MimeTypes    []string
	MimePrefixes []string
	Extensions   []string
}

type MaliciousContentScanner interface {
	ScanObject(ctx context.Context, req ScanObjectRequest) error
}

type ScanObjectRequest struct {
	FileID      string
	OwnerUserID string
	TargetType  string
	FileName    string
	MimeType    string
	SizeBytes   int64
	Bucket      string
	ObjectKey   string
	ContentHash *string
}

type noopMaliciousContentScanner struct{}

func (noopMaliciousContentScanner) ScanObject(context.Context, ScanObjectRequest) error {
	return nil
}

var targetGovernancePolicies = map[string]targetGovernancePolicy{
	"avatar": {
		TargetType: "avatar",
		MaxBytes:   8 * mib,
		MimeTypes:  []string{"image/jpeg", "image/png", "image/gif"},
		Extensions: []string{".jpg", ".jpeg", ".png", ".gif"},
	},
	"group_avatar": {
		TargetType: "group_avatar",
		MaxBytes:   8 * mib,
		MimeTypes:  []string{"image/jpeg", "image/png", "image/gif"},
		Extensions: []string{".jpg", ".jpeg", ".png", ".gif"},
	},
	"chat_image": {
		TargetType: "chat_image",
		MaxBytes:   32 * mib,
		MimeTypes:  []string{"image/jpeg", "image/png", "image/gif"},
		Extensions: []string{".jpg", ".jpeg", ".png", ".gif"},
	},
	"chat_video": {
		TargetType: "chat_video",
		MaxBytes:   512 * mib,
		MimeTypes:  []string{"video/mp4", "video/quicktime", "video/webm", "video/x-m4v"},
		Extensions: []string{".mp4", ".mov", ".webm", ".m4v"},
	},
	"chat_audio": {
		TargetType: "chat_audio",
		MaxBytes:   64 * mib,
		MimeTypes:  []string{"audio/aac", "audio/flac", "audio/mpeg", "audio/mp4", "audio/ogg", "audio/wav", "audio/webm", "audio/x-wav"},
		Extensions: []string{".aac", ".flac", ".m4a", ".mp3", ".ogg", ".opus", ".wav", ".weba"},
	},
	"chat_file": {
		TargetType: "chat_file",
		MaxBytes:   128 * mib,
		MimeTypes: []string{
			"application/json",
			"application/msword",
			"application/octet-stream",
			"application/pdf",
			"application/vnd.ms-excel",
			"application/vnd.ms-powerpoint",
			"application/vnd.openxmlformats-officedocument.presentationml.presentation",
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"application/zip",
			"text/csv",
			"text/markdown",
			"text/plain",
			"text/tab-separated-values",
			"text/xml",
		},
		Extensions: []string{".csv", ".doc", ".docx", ".json", ".log", ".md", ".pdf", ".ppt", ".pptx", ".tsv", ".txt", ".xls", ".xlsx", ".xml", ".zip"},
	},
	"post_image": {
		TargetType: "post_image",
		MaxBytes:   32 * mib,
		MimeTypes:  []string{"image/jpeg", "image/png", "image/gif"},
		Extensions: []string{".jpg", ".jpeg", ".png", ".gif"},
	},
	"post_video": {
		TargetType: "post_video",
		MaxBytes:   1024 * mib,
		MimeTypes:  []string{"video/mp4", "video/quicktime", "video/webm", "video/x-m4v"},
		Extensions: []string{".mp4", ".mov", ".webm", ".m4v"},
	},
	"post_live": {
		TargetType: "post_live",
		MaxBytes:   1024 * mib,
		MimeTypes:  []string{"image/jpeg", "image/png", "video/mp4", "video/quicktime"},
		Extensions: []string{".jpg", ".jpeg", ".png", ".mp4", ".mov"},
	},
	"post_media": {
		TargetType: "post_media",
		MaxBytes:   1024 * mib,
		MimeTypes:  []string{"image/jpeg", "image/png", "image/gif", "video/mp4", "video/quicktime", "video/webm", "video/x-m4v"},
		Extensions: []string{".jpg", ".jpeg", ".png", ".gif", ".mp4", ".mov", ".webm", ".m4v"},
	},
	"ai_upload": {
		TargetType: "ai_upload",
		MaxBytes:   maxAIParseSourceBytes,
		MimeTypes: []string{
			"application/json",
			"application/pdf",
			"application/vnd.openxmlformats-officedocument.presentationml.presentation",
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"text/csv",
			"text/html",
			"text/markdown",
			"text/plain",
			"text/tab-separated-values",
			"text/xml",
		},
		MimePrefixes: []string{"text/"},
		Extensions:   []string{".conf", ".csv", ".docx", ".go", ".h", ".html", ".java", ".js", ".json", ".log", ".md", ".pdf", ".pptx", ".py", ".rs", ".sql", ".ts", ".tsx", ".txt", ".xlsx", ".xml", ".yaml", ".yml"},
	},
}

var targetTypeAliases = map[string]string{
	"user_avatar":     "avatar",
	"chat_attachment": "chat_file",
}

func (s *Service) validateGovernance(ctx context.Context, userID string, targetType string, fileName string, mimeType string, sizeBytes int64) (string, string, error) {
	canonicalTarget, normalizedMime, err := validateTargetPolicy(targetType, fileName, mimeType, sizeBytes)
	if err != nil {
		return "", "", err
	}
	if err := s.checkUserQuota(ctx, userID, sizeBytes); err != nil {
		return "", "", err
	}
	return canonicalTarget, normalizedMime, nil
}

func validateTargetPolicy(targetType string, fileName string, mimeType string, sizeBytes int64) (string, string, error) {
	canonicalTarget := canonicalTargetType(targetType)
	policy, ok := targetGovernancePolicies[canonicalTarget]
	if !ok {
		return "", "", ErrValidation
	}
	normalizedMime := normalizeMimeType(mimeType)
	if normalizedMime == "" {
		return "", "", ErrFileTypeNotAllowed
	}
	if sizeBytes <= 0 {
		return "", "", ErrValidation
	}
	if sizeBytes > policy.MaxBytes {
		return "", "", fmt.Errorf("%w: %s limit is %d bytes", ErrFileTooLarge, policy.TargetType, policy.MaxBytes)
	}
	if !policy.allowsMime(normalizedMime) || !policy.allowsExtension(fileName, normalizedMime) {
		return "", "", ErrFileTypeNotAllowed
	}
	return canonicalTarget, normalizedMime, nil
}

func (s *Service) checkUserQuota(ctx context.Context, userID string, incomingBytes int64) error {
	if s.cfg.UserQuotaBytes <= 0 || s.repos.Files == nil {
		return nil
	}
	usedBytes, err := s.repos.Files.SumActiveBytesByOwner(ctx, userID)
	if err != nil {
		return err
	}
	if usedBytes+incomingBytes > s.cfg.UserQuotaBytes {
		return fmt.Errorf("%w: user quota is %d bytes", ErrFileQuotaExceeded, s.cfg.UserQuotaBytes)
	}
	return nil
}

func (s *Service) scanUploadedObject(ctx context.Context, file repository.File, targetType string) error {
	scanner := s.scanner
	if scanner == nil {
		scanner = noopMaliciousContentScanner{}
	}
	ownerUserID := ""
	if file.OwnerUserID != nil {
		ownerUserID = *file.OwnerUserID
	}
	err := scanner.ScanObject(ctx, ScanObjectRequest{
		FileID:      file.ID,
		OwnerUserID: ownerUserID,
		TargetType:  canonicalTargetType(targetType),
		FileName:    file.OriginalFileName,
		MimeType:    file.MimeType,
		SizeBytes:   file.SizeBytes,
		Bucket:      file.Bucket,
		ObjectKey:   file.ObjectKey,
		ContentHash: file.ContentHash,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrFileScanRejected, err)
	}
	return nil
}

func canonicalTargetType(targetType string) string {
	normalized := strings.ToLower(strings.TrimSpace(targetType))
	if alias, ok := targetTypeAliases[normalized]; ok {
		return alias
	}
	return normalized
}

func normalizeMimeType(value string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return strings.ToLower(mediaType)
}

func (p targetGovernancePolicy) allowsMime(mimeType string) bool {
	if slices.Contains(p.MimeTypes, mimeType) {
		return true
	}
	for _, prefix := range p.MimePrefixes {
		if strings.HasPrefix(mimeType, prefix) {
			return true
		}
	}
	return false
}

func (p targetGovernancePolicy) allowsExtension(fileName string, mimeType string) bool {
	extension := strings.ToLower(filepath.Ext(fileName))
	if extension == "" {
		return strings.HasPrefix(mimeType, "text/")
	}
	return slices.Contains(p.Extensions, extension)
}

func sniffedMimeType(prefix []byte, declaredMime string) string {
	sniffed := normalizeMimeType(http.DetectContentType(prefix))
	declared := normalizeMimeType(declaredMime)
	switch {
	case sniffed == "application/octet-stream":
		return declared
	case sniffed == "application/zip" && strings.HasPrefix(declared, "application/vnd.openxmlformats-officedocument."):
		return declared
	case sniffed == "text/plain" && (strings.HasPrefix(declared, "text/") || declared == "application/json"):
		return declared
	default:
		return sniffed
	}
}
