# Opencode Telegram

Telegram-driven control plane for local OpenCode execution through a backend and a local agent daemon.

Current version: `0.2.0` (stored in `VERSION`).

## What this repository contains

- `cmd/opencode-bot`: Telegram bot process.
- `cmd/oct-backend`: backend API (`/v1/pair/*`, `/v1/command`, `/v1/poll`, `/v1/result`, project/result helpers).
- `cmd/oct-agent`: local daemon that long-polls backend and executes commands.
- `internal/bot`: Telegram command handlers, approval UX, backend routing, Opencode client integration.
- `internal/backend`: pairing state, queue abstraction, Redis queue implementation, HTTP handlers.
- `internal/agent`: command dispatcher, policy enforcement, port allocation, OpenCode lifecycle.
- `internal/proxy/contracts`: shared command/result contracts and validation.
- `pkg/store`: in-memory store interfaces/implementation used by the bot.

## Documentation

- Docs hub: `docs/README.md`
- Spec index: `docs/spec/spec-index.md`
- MVP feature spec: `docs/spec/features/telegram-backend-daemon-mvp.md`

## Prerequisites

- Go `1.20+`
- Redis (default expected at `redis://localhost:6379`)
- Telegram bot token from BotFather
- `opencode` CLI available on the machine where `oct-agent` runs
- Optional: `task` command runner

## Environment variables

### Bot (`cmd/opencode-bot`)

- Required:
  - `TELEGRAM_BOT_TOKEN`
- Common:
  - `OCT_BACKEND_URL` (default `http://localhost:8080`)
  - `ALLOWED_TELEGRAM_IDS`
  - `ADMIN_TELEGRAM_IDS`
  - `OPENCODE_BASE_URL` (used by existing bot paths)
  - `OPENCODE_AUTH_TOKEN`
  - `SESSION_PREFIX` (default `oct_`)
  - `TELEGRAM_MODE` (only `polling` is implemented)

### Backend (`cmd/oct-backend`)

- `OCT_BACKEND_ADDR` (default `:8080`)
- `REDIS_URL` (default `redis://localhost:6379`)

### Agent (`cmd/oct-agent`)

- Required:
  - `OCT_AGENT_KEY`
- Optional:
  - `OCT_AGENT_ID`
  - `OCT_BACKEND_URL` (default `http://localhost:8080`)
  - `OCT_AGENT_ADDR` (default `:9090`)

## First 15 minutes (fresh machine)

1. Install dependencies and verify toolchain:

```bash
go version
redis-server --version
```

2. Copy env template and set bot values:

```bash
cp .env.example .env
```

3. Run baseline tests:

```bash
go test ./...
```

4. Start backend:

```bash
go run ./cmd/oct-backend
```

5. Start bot:

```bash
go run ./cmd/opencode-bot
```

6. Pair and run agent (after obtaining `agent_key` via pairing flow):

```bash
OCT_AGENT_KEY=<agent_key> go run ./cmd/oct-agent
```

## Common development commands

Using Go directly:

- Build all: `go build -v ./...`
- Test all: `go test -v ./...`
- Coverage gate: `go test -covermode=count -coverprofile=coverage.out ./internal/... ./pkg/... && go run ./cmd/coveragecheck -file coverage.out -min 50`

Using Task (bot-oriented helpers already present in repo):

- `task dev`
- `task test`
- `task lint`
- `task ci`

## Release workflow

- CI checks are in `.github/workflows/go.yml`.
- Tag push workflow is in `.github/workflows/release.yml`.
- Pushing a tag `vX.Y.Z` (for example `v0.2.0`) triggers:
  - test verification,
  - cross-platform `oct-agent` build,
  - archive packaging (`.tar.gz` / `.zip`),
  - `SHA256SUMS` generation,
  - GitHub Release publish with artifacts.

## Versioning

- Source of truth: `VERSION`.
- Current version: `0.2.0`.
- Release tags must match versioning format `vX.Y.Z`.

## Troubleshooting

- `TELEGRAM_BOT_TOKEN is required`
  - Set `TELEGRAM_BOT_TOKEN` before running `cmd/opencode-bot`.

- `redis init error` on backend start
  - Verify Redis is running and `REDIS_URL` is reachable.

- Agent exits with `OCT_AGENT_KEY is required`
  - Complete pairing first and export `OCT_AGENT_KEY`.

- Bot returns "Unknown project alias"
  - Use `/project list` and run commands with the registered alias.

- Commands time out during `start_server`/`run_task`
  - Confirm `opencode` binary exists and local health endpoint is reachable by agent.
