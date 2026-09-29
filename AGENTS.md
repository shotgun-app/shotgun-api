# AGENTS.md

Guidance for AI coding agents working in this repository. See `README.md` for what the project is.

## Stack

Go 1.26 + Gin, PostgreSQL 17, golang-migrate, Docker Compose. Go does not need to be installed locally: everything runs in Docker.

## Layout

- `main.go` - entry point. `setupRouter` registers middleware and routes and is used by tests
- `config/` - settings read from environment variables, with defaults for local development
- `handlers/` - HTTP handlers, one file per resource (`auth.go`, `session.go` for the session cookie and `RequireAuth`)
- `middleware/` - Gin middleware (CORS)
- `migrations/` - SQL migrations, applied by the `migrate` service on start

## Commands

- Start or rebuild: `docker compose up -d --build`
- Tests and vet: `docker compose run --rm --build test`
- Database shell: `docker compose exec db psql -U shotgun`
- Wipe the database: `docker compose down -v`

Run the tests before finishing a change.

## Migrations

- Never edit a migration that is already on `main`. Add a new pair with the next number: `000002_name.up.sql` and `000002_name.down.sql`
- `down.sql` must revert exactly what `up.sql` does
- Emails are unique case-insensitively (`users_email_lower_idx`). Compare them with `lower(email) = lower($1)`
- A passenger can have only one `confirmed` booking per ride (`bookings_active_idx`). Cancelled bookings stay as history

## Conventions

- Configuration comes from environment variables via `config.Load()`. New settings also go in `.env.example` and `docker-compose.yml`
- Tests use `net/http/httptest` against the router, with no running server. Auth integration tests (`auth_test.go`) use the real database and skip unless `TEST_DATABASE_URL` is set (the compose `test` service sets it)
- Handlers reply with `{"message": "..."}` on errors; the web app reads that field
- Comments explain why, not what. Keep them short

## Commits

- Do not add `Co-Authored-By` trailers or any other attribution for AI agents to commit messages or pull requests
