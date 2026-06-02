package tests

import (
	"errors"
	"testing"

	"nlserver/internal/config"
	"nlserver/internal/storage"
)

func TestStorageProviderFactoryDisabled(t *testing.T) {
	provider, err := storage.NewProvider(config.StorageConfig{Provider: "disabled"})
	if provider != nil {
		t.Fatal("provider is not nil, want nil")
	}
	if !errors.Is(err, storage.ErrProviderDisabled) {
		t.Fatalf("error = %v, want ErrProviderDisabled", err)
	}
}

func TestStorageProviderFactoryMinIO(t *testing.T) {
	provider, err := storage.NewProvider(config.StorageConfig{
		Provider:         "minio",
		Endpoint:         "http://localhost:9000",
		InternalEndpoint: "http://localhost:9000",
		AccessKey:        "minio",
		SecretKey:        "miniosecretminiosecret",
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if provider == nil {
		t.Fatal("provider is nil, want configured provider")
	}
}

func TestStorageProviderFactoryUnsupported(t *testing.T) {
	provider, err := storage.NewProvider(config.StorageConfig{Provider: "unknown"})
	if provider != nil {
		t.Fatal("provider is not nil, want nil")
	}
	if !errors.Is(err, storage.ErrProviderUnsupported) {
		t.Fatalf("error = %v, want ErrProviderUnsupported", err)
	}
}
