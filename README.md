<div align="center">
  <img src="https://raw.githubusercontent.com/shotgun-app/.github/main/content/logo.svg" alt="Shotgun App logo" height="70" />
  <h1 align="center">shotgun-api</h1>
</div>

Backend API for **Shotgun**, a carpooling app: drivers publish trips they are
driving anyway, passengers book the empty seats.

This repo holds the Go API that serves the Vue 3 front end in
[`shotgun-web`](https://github.com/shotgun-app/shotgun-web) and manages users, trips and bookings.

## Status

Early stage. The API has a `/ping` endpoint and a PostgreSQL database with the
initial schema. The front end still runs against an in-memory mock. Next up is
user authentication (register, login, logout and current user).

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
- Run tests: `docker compose run --rm --build test`
- New migration: add `migrations/000002_name.up.sql` and `migrations/000002_name.down.sql`, then run the start command again.
- Undo the last migration: `docker compose run --rm migrate down 1`

## Related repositories

- [`shotgun-web`](https://github.com/shotgun-app/shotgun-web) - Vue 3 web client
- [`.github`](https://github.com/shotgun-app/.github) - organization profile and project docs
