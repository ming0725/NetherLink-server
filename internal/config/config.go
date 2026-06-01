package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env      string
	HTTP     HTTPConfig
	Auth     AuthConfig
	Postgres PostgresConfig
	Redis    RedisConfig
	Storage  StorageConfig
}

type HTTPConfig struct {
	Addr string
}

type AuthConfig struct {
	TokenSecret     string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

type PostgresConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
	SSLMode  string
}

type RedisConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	Database int
}

type StorageConfig struct {
	Provider         string
	Endpoint         string
	InternalEndpoint string
	AccessKey        string
	SecretKey        string
	UseSSL           bool
	PresignTTL       time.Duration
	Buckets          StorageBuckets
}

type StorageBuckets struct {
	Public    string
	Protected string
	Private   string
	Tmp       string
}

func Load() (Config, error) {
	var validationErrors []string

	cfg := Config{
		Env: getEnv("NETHERLINK_ENV", "local"),
		HTTP: HTTPConfig{
			Addr: getEnv("NETHERLINK_HTTP_ADDR", ":8080"),
		},
		Auth: AuthConfig{
			TokenSecret:     requiredEnv(&validationErrors, "NETHERLINK_AUTH_TOKEN_SECRET"),
			AccessTokenTTL:  getEnvDuration(&validationErrors, "NETHERLINK_AUTH_ACCESS_TOKEN_TTL", 15*time.Minute),
			RefreshTokenTTL: getEnvDuration(&validationErrors, "NETHERLINK_AUTH_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		},
		Postgres: PostgresConfig{
			Host:     getEnv("NETHERLINK_POSTGRES_HOST", "localhost"),
			Port:     getEnvPort(&validationErrors, "NETHERLINK_POSTGRES_PORT", 5432),
			Database: getEnv("NETHERLINK_POSTGRES_DB", "netherlink"),
			User:     getEnv("NETHERLINK_POSTGRES_USER", "netherlink_app"),
			Password: requiredEnv(&validationErrors, "NETHERLINK_POSTGRES_PASSWORD"),
			SSLMode:  getEnv("NETHERLINK_POSTGRES_SSLMODE", "disable"),
		},
		Redis: RedisConfig{
			Host:     getEnv("NETHERLINK_REDIS_HOST", "localhost"),
			Port:     getEnvPort(&validationErrors, "NETHERLINK_REDIS_PORT", 6379),
			Username: getEnv("NETHERLINK_REDIS_USERNAME", ""),
			Password: getEnv("NETHERLINK_REDIS_PASSWORD", ""),
			Database: getEnvNonNegativeInt(&validationErrors, "NETHERLINK_REDIS_DB", 0),
		},
		Storage: StorageConfig{
			Provider:         getEnv("NETHERLINK_OBJECT_STORAGE_PROVIDER", "minio"),
			Endpoint:         getEnv("NETHERLINK_OBJECT_STORAGE_ENDPOINT", "http://localhost:9000"),
			InternalEndpoint: getEnv("NETHERLINK_OBJECT_STORAGE_INTERNAL_ENDPOINT", "http://minio:9000"),
			AccessKey:        requiredEnv(&validationErrors, "NETHERLINK_OBJECT_STORAGE_ACCESS_KEY"),
			SecretKey:        requiredEnv(&validationErrors, "NETHERLINK_OBJECT_STORAGE_SECRET_KEY"),
			UseSSL:           getEnvBool(&validationErrors, "NETHERLINK_OBJECT_STORAGE_USE_SSL", false),
			PresignTTL:       getEnvDuration(&validationErrors, "NETHERLINK_OBJECT_STORAGE_PRESIGN_TTL", 10*time.Minute),
			Buckets: StorageBuckets{
				Public:    getEnv("NETHERLINK_OBJECT_STORAGE_PUBLIC_BUCKET", "netherlink-public"),
				Protected: getEnv("NETHERLINK_OBJECT_STORAGE_PROTECTED_BUCKET", "netherlink-protected"),
				Private:   getEnv("NETHERLINK_OBJECT_STORAGE_PRIVATE_BUCKET", "netherlink-private"),
				Tmp:       getEnv("NETHERLINK_OBJECT_STORAGE_TMP_BUCKET", "netherlink-tmp"),
			},
		},
	}

	validate(cfg, &validationErrors)
	if len(validationErrors) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %s", strings.Join(validationErrors, "; "))
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func requiredEnv(validationErrors *[]string, key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		*validationErrors = append(*validationErrors, key+" is required")
	}
	return value
}

func getEnvPort(validationErrors *[]string, key string, fallback int) int {
	value, ok := parseEnvInt(validationErrors, key, fallback)
	if !ok {
		return fallback
	}
	if value < 1 || value > 65535 {
		*validationErrors = append(*validationErrors, key+" must be a TCP port between 1 and 65535")
		return fallback
	}
	return value
}

func getEnvNonNegativeInt(validationErrors *[]string, key string, fallback int) int {
	value, ok := parseEnvInt(validationErrors, key, fallback)
	if !ok {
		return fallback
	}
	if value < 0 {
		*validationErrors = append(*validationErrors, key+" must be non-negative")
		return fallback
	}
	return value
}

func parseEnvInt(validationErrors *[]string, key string, fallback int) (int, bool) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, true
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		*validationErrors = append(*validationErrors, key+" must be an integer")
		return fallback, false
	}
	return parsed, true
}

func getEnvBool(validationErrors *[]string, key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		*validationErrors = append(*validationErrors, key+" must be a boolean")
		return fallback
	}
	return parsed
}

func getEnvDuration(validationErrors *[]string, key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		*validationErrors = append(*validationErrors, key+" must be a duration such as 15m or 720h")
		return fallback
	}
	if parsed <= 0 {
		*validationErrors = append(*validationErrors, key+" must be greater than zero")
		return fallback
	}
	return parsed
}

func validate(cfg Config, validationErrors *[]string) {
	if cfg.Auth.TokenSecret != "" && len(cfg.Auth.TokenSecret) < 32 {
		*validationErrors = append(*validationErrors, "NETHERLINK_AUTH_TOKEN_SECRET must be at least 32 characters")
	}
	if cfg.Postgres.Password != "" && len(cfg.Postgres.Password) < 16 {
		*validationErrors = append(*validationErrors, "NETHERLINK_POSTGRES_PASSWORD must be at least 16 characters")
	}
	if cfg.Storage.AccessKey != "" && len(cfg.Storage.AccessKey) < 3 {
		*validationErrors = append(*validationErrors, "NETHERLINK_OBJECT_STORAGE_ACCESS_KEY must be at least 3 characters")
	}
	if cfg.Storage.SecretKey != "" && len(cfg.Storage.SecretKey) < 16 {
		*validationErrors = append(*validationErrors, "NETHERLINK_OBJECT_STORAGE_SECRET_KEY must be at least 16 characters")
	}
	if cfg.Env == "production" {
		if cfg.Postgres.SSLMode == "disable" {
			*validationErrors = append(*validationErrors, "NETHERLINK_POSTGRES_SSLMODE must not be disable in production")
		}
		if !cfg.Storage.UseSSL {
			*validationErrors = append(*validationErrors, "NETHERLINK_OBJECT_STORAGE_USE_SSL must be true in production")
		}
	}
}
