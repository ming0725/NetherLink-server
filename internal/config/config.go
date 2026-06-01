package config

import (
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

func Load() Config {
	return Config{
		Env: getEnv("NETHERLINK_ENV", "local"),
		HTTP: HTTPConfig{
			Addr: getEnv("NETHERLINK_HTTP_ADDR", ":8080"),
		},
		Auth: AuthConfig{
			TokenSecret:     getEnv("NETHERLINK_AUTH_TOKEN_SECRET", "nlserver-local-dev-token-secret-change-me"),
			AccessTokenTTL:  getEnvDuration("NETHERLINK_AUTH_ACCESS_TOKEN_TTL", 15*time.Minute),
			RefreshTokenTTL: getEnvDuration("NETHERLINK_AUTH_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		},
		Postgres: PostgresConfig{
			Host:     getEnv("NETHERLINK_POSTGRES_HOST", "localhost"),
			Port:     getEnvInt("NETHERLINK_POSTGRES_PORT", 5432),
			Database: getEnv("NETHERLINK_POSTGRES_DB", "netherlink"),
			User:     getEnv("NETHERLINK_POSTGRES_USER", "netherlink_app"),
			Password: getEnv("NETHERLINK_POSTGRES_PASSWORD", "nl_pg_v1_Pq9xJ4mR7tL2cW8s"),
			SSLMode:  getEnv("NETHERLINK_POSTGRES_SSLMODE", "disable"),
		},
		Redis: RedisConfig{
			Host:     getEnv("NETHERLINK_REDIS_HOST", "localhost"),
			Port:     getEnvInt("NETHERLINK_REDIS_PORT", 6379),
			Username: getEnv("NETHERLINK_REDIS_USERNAME", ""),
			Password: getEnv("NETHERLINK_REDIS_PASSWORD", ""),
			Database: getEnvInt("NETHERLINK_REDIS_DB", 0),
		},
		Storage: StorageConfig{
			Provider:         getEnv("NETHERLINK_OBJECT_STORAGE_PROVIDER", "minio"),
			Endpoint:         getEnv("NETHERLINK_OBJECT_STORAGE_ENDPOINT", "http://localhost:9000"),
			InternalEndpoint: getEnv("NETHERLINK_OBJECT_STORAGE_INTERNAL_ENDPOINT", "http://minio:9000"),
			AccessKey:        getEnv("NETHERLINK_OBJECT_STORAGE_ACCESS_KEY", "netherlink_storage_admin"),
			SecretKey:        getEnv("NETHERLINK_OBJECT_STORAGE_SECRET_KEY", "nl_minio_v1_N8sT3mQ6vX9pL2wR"),
			UseSSL:           getEnvBool("NETHERLINK_OBJECT_STORAGE_USE_SSL", false),
			PresignTTL:       getEnvDuration("NETHERLINK_OBJECT_STORAGE_PRESIGN_TTL", 10*time.Minute),
			Buckets: StorageBuckets{
				Public:    getEnv("NETHERLINK_OBJECT_STORAGE_PUBLIC_BUCKET", "netherlink-public"),
				Protected: getEnv("NETHERLINK_OBJECT_STORAGE_PROTECTED_BUCKET", "netherlink-protected"),
				Private:   getEnv("NETHERLINK_OBJECT_STORAGE_PRIVATE_BUCKET", "netherlink-private"),
				Tmp:       getEnv("NETHERLINK_OBJECT_STORAGE_TMP_BUCKET", "netherlink-tmp"),
			},
		},
	}
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
