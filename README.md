# Opencode Telegram Bot

Telegram bot that routes chat commands/messages to an Opencode server and streams updates back into Telegram.

## What this repo contains

- `cmd/opencode-bot/main.go`: process entrypoint (config + wiring + polling/event loops).
- `internal/bot`: application logic (Telegram command handlers, Opencode HTTP/SSE client, config parsing, debounce/event glue).
- `pkg/store`: in-memory session mapping abstraction used by handlers.
- `Taskfile.yml`: local command entrypoints for build/test/lint/coverage.
- `.github/workflows/go.yml`: CI quality gates (build, tests, lint, coverage threshold).

## Documentation

- Documentation hub: `docs/README.md`
- Spec index: `docs/spec/spec-index.md`
- Local development runbook: `docs/spec/runbooks/local-dev.md`

## Prerequisites

- Go 1.20+
- [Task](https://taskfile.dev/installation/) (recommended command runner for this repo)
- Telegram bot token from [@BotFather](https://t.me/botfather)
- Reachable Opencode server
- Optional: `golangci-lint` (auto-installed via `task lint:install`)

## Setup

1. Copy env template:

```bash
cp .env.example .env
```

2. Set required values in `.env`:

```env
TELEGRAM_BOT_TOKEN=your-bot-token
OPENCODE_BASE_URL=http://localhost:4096
ALLOWED_TELEGRAM_IDS=your-telegram-user-id
ADMIN_TELEGRAM_IDS=your-telegram-user-id
```

3. Build once:

```bash
task build
```

4. Run locally:

```bash
task dev
```

## Run, Test, Lint, Coverage

- Run app: `task dev`
- Build binary: `task build`
- Unit tests: `task test`
- Verbose tests: `task test:verbose`
- Install pinned linter binary: `task lint:install`
- Lint: `task lint`
- Coverage profile + report input: `task coverage`
- Coverage quality gate (>= 90% on `internal/...` and `pkg/...`): `task coverage:check`
- HTML coverage report (after `task coverage`): `task coverage:html`
- CI-parity local check (same intent as `.github/workflows/go.yml`): `task ci`

CI runs the same quality intent: build, `go test`, `golangci-lint`, and coverage threshold validation.

If you do not want to install Task, run direct Go commands instead:

- Build (CI-style): `go build -v ./...`
- Unit tests (CI-style): `go test -v ./...`
- Lint: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 && "$(go env GOPATH)/bin/golangci-lint" run --config .golangci.yml ./...`
- Coverage gate: `go test -covermode=count -coverprofile=coverage.out ./internal/... ./pkg/... && go run ./cmd/coveragecheck -file coverage.out -min 90`

## First 15 Minutes Checklist

1. Confirm toolchain: `go version` (1.20+).
2. Install Task (`task --version`) or use the direct Go commands listed above.
3. Copy `.env.example` to `.env` and set real values.
4. Run `task test` to confirm baseline health.
5. Run `task lint:install` once, then `task lint`.
6. Run `task ci` to mirror CI checks locally.
7. Start app with `task dev` and send `/status` from an allowed Telegram user.

## Troubleshooting

- `TELEGRAM_BOT_TOKEN is required`
  - `.env` is missing or not loaded; set `TELEGRAM_BOT_TOKEN` and retry.

- `telegram bot init error`
  - Token is invalid or network cannot reach Telegram API.

- `event listener error` / no streaming edits
  - Check `OPENCODE_BASE_URL` reachability and Opencode event endpoint behavior.

- `task lint` fails with `command not found: golangci-lint`
  - Run `task lint:install`. `task lint` also falls back to `$(go env GOPATH)/bin/golangci-lint` if not on `PATH`.
  - Optional: add `$(go env GOPATH)/bin` to your shell `PATH` for direct `golangci-lint` usage.

- Coverage gate fails
  - Run `task coverage:check` locally; target uncovered logic in `internal/bot` first.

## How to Program in This Go Project

- Keep behavior in `internal/bot`; keep `cmd/opencode-bot/main.go` as wiring only.
- Treat Telegram handlers (`handleStatus`, `handleRun`, etc.) as command boundaries: parse inputs, call `OpencodeClientInterface`, and emit user-visible text.
- Preserve interface seams for testability:
  - Telegram calls through `TelegramBotInterface`
  - Opencode calls through `OpencodeClientInterface`
  - persistence through `store.Store`
- When adding command behavior:
  1. Add/adjust a focused unit test in `internal/bot/*_test.go`.
  2. Implement minimal handler/client change.
  3. Run `task test`, `task lint`, `task coverage:check`.
- Prefer deterministic tests:
  - Use mock clients/bots instead of real network calls.
  - Keep async tests bounded (channels/timeouts) and avoid long sleeps.
- Keep diffs small and reversible; avoid unrelated refactors while touching quality gates.

## Environment Variables

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `TELEGRAM_BOT_TOKEN` | yes | - | Bot token from BotFather |
| `OPENCODE_BASE_URL` | no | `http://localhost:4096` | Opencode server URL |
| `OPENCODE_AUTH_TOKEN` | no | - | Optional bearer token for Opencode |
| `ALLOWED_TELEGRAM_IDS` | no | - | Comma/space-separated user IDs; empty allows all |
| `ADMIN_TELEGRAM_IDS` | no | - | Comma/space-separated admin IDs |
| `SESSION_PREFIX` | no | `oct_` | Prefix used for persistent session discovery/creation |
| `TELEGRAM_MODE` | no | `polling` | Polling is implemented; webhook is not yet implemented |
| `PORT` | no | `3000` | Reserved listen port for webhook mode |
| `REDIS_URL` | no | - | Reserved for non-memory store implementation |
