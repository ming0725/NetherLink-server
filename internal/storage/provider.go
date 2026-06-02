package storage

import (
	"errors"
	"fmt"
	"strings"

	"nlserver/internal/config"
)

var (
	ErrProviderDisabled       = errors.New("storage: provider disabled")
	ErrProviderNotImplemented = errors.New("storage: provider not implemented")
	ErrProviderUnsupported    = errors.New("storage: provider unsupported")
)

func NewProvider(cfg config.StorageConfig) (ObjectStorageProvider, error) {
	switch normalizeProvider(cfg.Provider) {
	case "", "none", "disabled":
		return nil, ErrProviderDisabled
	case "minio", "s3":
		return newMinIOProvider(cfg)
	default:
		return nil, fmt.Errorf("%w: %s", ErrProviderUnsupported, cfg.Provider)
	}
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
