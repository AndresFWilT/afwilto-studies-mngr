# afwilto-studies-mngr

Backend for **Studies**, the personal knowledge base: hierarchical categories, subjects,
and Markdown notes with kinds, tags and links to slab. Go + PostgreSQL 17, hexagonal
architecture (`backend/CLAUDE.md`).

- Spec: `frontend/docs/specs/16-studies.md` (slices under `16-studies/`)
- Contract: [`api/openapi.yaml`](api/openapi.yaml), the source of truth for every endpoint
- Port: **8084**

## Rule worth knowing: deletion is always logical

Every `DELETE` endpoint sets `deleted_at`; nothing ever issues a SQL `DELETE`. This is
enforced twice:

1. **Database:** every table has a `BEFORE DELETE` trigger calling
   `forbid_hard_delete()` (migration `0001`), which raises SQLSTATE `23001`. `TRUNCATE`
   is deliberately still allowed (test resets, admin wipes).
2. **Static test:** `internal/studies/adapter/out/postgres/no_hard_delete_test.go` fails
   if any repository source contains `DELETE FROM`.

To recover a deleted row: `UPDATE <table> SET deleted_at = NULL WHERE id = '…';`.

## Seed data

Migration `0004` seeds the note kinds Summary, Cheat sheet and Notes. Migration
`0008` (spec 17) seeds **TI → Networks → CIDR** with the note "IPv4 and IPv6 CIDR — guide and
tables", which links to the slab CIDR Calculator.

- The seed reuses existing alive rows by name and never modifies them.
- Rows it creates have fixed ids `00000000-0000-7000-8017-00000000000N`.
- Its down migration logically deletes only those rows.

## Run

```bash
# whole platform (from the lab root)
docker compose up -d studies-db studies-mngr
curl localhost:8084/healthz   # {"data":{"status":"ok"}}

# local binary against your own Postgres
DATABASE_URL=postgres://studies:studies@localhost:5432/studies?sslmode=disable make run
```

| Env var | Default | Purpose |
|---|---|---|
| `PORT` | `8084` | HTTP port |
| `DATABASE_URL` | — | Postgres DSN |
| `ALLOW_ORIGINS` | `http://localhost:3000,http://localhost:3008` | CORS allowlist (shell + standalone MFE) |
| `MAX_BODY_BYTES` | `1048576` | Request body cap (413 above it) |
| `MIGRATE_ON_START` | `true` | Apply `migrations/` at startup |

## Test

```bash
make test        # everything; integration tests start Postgres via testcontainers (Docker required)
make test-short  # unit + static + contract tests only, no Docker
```

## Layout

```
cmd/api            composition root + startup migrations
cmd/healthcheck    static probe binary for the distroless HEALTHCHECK
configs            env parsing
api/openapi.yaml   the contract
migrations         golang-migrate SQL pairs
internal/studies
  domain           pure Go (import allowlist enforced by arch_test.go)
  app/port/{in,out}, app/usecase   (from Slice 02)
  adapter/in/http  stdlib net/http router, middleware, handlers
  adapter/out/postgres, adapter/out/system
```
