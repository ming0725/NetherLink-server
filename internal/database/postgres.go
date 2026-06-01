package database

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"nlserver/internal/config"
)

func OpenPostgres(ctx context.Context, cfg config.PostgresConfig) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(PostgresDSN(cfg))
	if err != nil {
		return nil, err
	}

	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	return pgxpool.NewWithConfig(ctx, poolConfig)
}

func PostgresDSN(cfg config.PostgresConfig) string {
	dsn := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   "/" + cfg.Database,
	}

	query := dsn.Query()
	query.Set("sslmode", cfg.SSLMode)
	dsn.RawQuery = query.Encode()

	return dsn.String()
}
