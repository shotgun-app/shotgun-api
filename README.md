<div align="center">
  <img src="https://raw.githubusercontent.com/shotgun-app/.github/main/content/logo.svg" alt="Shotgun App logo" height="70" />
  <h1 align="center">shotgun-api</h1>
</div>

Backend API for **Shotgun**, a carpooling app: drivers publish trips they are
driving anyway, passengers book the empty seats.

This repo holds the Go API that serves the Vue 3 front end in
[`shotgun-web`](https://github.com/shotgun-app/shotgun-web) and manages users, trips and bookings.

## Status

Authentication works: register, login, logout, current user, and updating or
deleting the account. Trips and bookings endpoints do not exist yet.

## Authentication

Sessions live in the `sessions` table. Login and register create a row with a
random 32-byte token and set it in an HttpOnly `session` cookie (SameSite=Lax);
logout deletes the row and clears the cookie. Passwords are hashed with bcrypt.
Errors are JSON: `{"message": "..."}`.

| Method   | Path             | Auth | Notes                                                       |
| -------- | ---------------- | ---- | ----------------------------------------------------------- |
| `POST`   | `/auth/register` | no   | `{name, email, password, phone?}`, `201 {user}`, `409` duplicate email |
| `POST`   | `/auth/login`    | no   | `{email, password}`, `200 {user}`, `401` wrong credentials  |
| `POST`   | `/auth/logout`   | no   | `204`, always clears the cookie                             |
| `GET`    | `/auth/me`       | yes  | `200 {user}`, `401` without a valid session                 |
| `PATCH`  | `/auth/me`       | yes  | `{name?, email?, phone?}`, `200 {user}`, `409` duplicate email |
| `DELETE` | `/auth/me`       | yes  | deletes the account and its sessions, `204`                 |
| `POST`   | `/auth/password` | yes  | `{currentPassword, newPassword}`, `204`, `401` wrong current password; ends all other sessions |

`user` is `{id, name, email, phone, joinedAt}`. `phone` is E.164 (`+38640123456`) or `null`; on `PATCH` an empty string clears it. Settings (`DATABASE_URL`,
`SESSION_TTL_HOURS`, `COOKIE_SECURE`, `ALLOWED_ORIGIN`) are in `.env.example`.
Set `COOKIE_SECURE=true` when serving over HTTPS.

## Tech stack

- [Go](https://go.dev/) 1.26 with the [Gin](https://gin-gonic.com/) web framework
- [PostgreSQL](https://www.postgresql.org/) 17
- [golang-migrate](https://github.com/golang-migrate/migrate) for SQL migrations
- [Docker Compose](https://docs.docker.com/compose/) to run everything locally

## Getting started

Install [Docker](https://docs.docker.com/get-started/get-docker/) and make sure it is running. Then run:

```sh
docker compose up -d --build
```

This starts Postgres, applies the migrations and starts the API. Open http://localhost:8080/ping. It shows `pong`.

If you get `address already in use` for port 5432, a local Postgres is already running on it. Copy `.env.example` to `.env` and set `DB_PORT=5433` (or any free port).

### Commands

- After changing code: run the start command again. It rebuilds the API.
- Stop: `docker compose down`
- Wipe all data: `docker compose down -v`
- Run tests: `docker compose run --rm --build test` (includes integration tests against the database)
- New migration: add `migrations/000002_name.up.sql` and `migrations/000002_name.down.sql`, then run the start command again.
- Undo the last migration: `docker compose run --rm migrate down 1`

Each migration has two files so it can be undone: `up.sql` applies the change and `down.sql` reverts it exactly.

## Related repositories

- [`shotgun-web`](https://github.com/shotgun-app/shotgun-web) - Vue 3 web client
- [`.github`](https://github.com/shotgun-app/.github) - organization profile and project docs
