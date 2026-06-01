# PostgreSQL module

This module is the first backend foundation for NetherLink. It contains only the database bootstrapping layer:

- Docker Compose service for PostgreSQL.
- Init SQL mounted into `/docker-entrypoint-initdb.d`.
- No migration framework is selected yet. Add one when the Go service layer starts using migrations.

## Start

```sh
cp .env.example .env
docker compose up -d postgres
docker compose ps
```

## Connect

```sh
docker exec -it nlserver-postgres psql -U netherlink -d netherlink
```

## Reset local data

```sh
docker compose down -v
docker compose up -d postgres
```

The init SQL runs only when the PostgreSQL data volume is first created.
