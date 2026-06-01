# PostgreSQL module

This module is the first backend foundation for NetherLink. It contains only the database bootstrapping layer:

- Docker Compose service for PostgreSQL.
- Init SQL mounted into `/docker-entrypoint-initdb.d`.
- Go service readiness checks currently ping PostgreSQL through the configured connection pool.
- No migration framework is selected yet. Add one before schema changes need ordered, repeatable upgrades.

## Start

```sh
cp .env.example .env
docker compose up -d postgres
docker compose ps
```

The API server reads the same `NETHERLINK_POSTGRES_*` variables:

```sh
go run ./cmd/nlserver
curl http://localhost:8080/readyz
```

## Connect

```sh
docker exec -it nlserver-postgres psql -U netherlink_app -d netherlink
```

## Reset local data

```sh
docker compose down -v
docker compose up -d postgres
```

The init SQL runs only when the PostgreSQL data volume is first created.
