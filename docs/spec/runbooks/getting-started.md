# Getting Started

Links:
- [Spec index](../spec-index.md)

## Prerequisites

- Go 1.20+
- Telegram bot token from [@BotFather](https://t.me/botfather)
- Reachable Opencode server
- Optional: [Task](https://taskfile.dev/installation/)

## Quick Setup

1. Copy environment template:

```bash
cp .env.example .env
```

2. Fill required variables in `.env`:

```env
TELEGRAM_BOT_TOKEN=your-token
OPENCODE_BASE_URL=http://localhost:4096
ALLOWED_TELEGRAM_IDS=123456789
ADMIN_TELEGRAM_IDS=123456789
```

3. Run quality checks:

```bash
go test ./...
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
"$(go env GOPATH)/bin/golangci-lint" run --config .golangci.yml ./...
go test -covermode=count -coverprofile=coverage.out ./internal/... ./pkg/...
go run ./cmd/coveragecheck -file coverage.out -min 90
```

4. Start bot:

```bash
go run ./cmd/opencode-bot
```

Task shortcuts (optional):

```bash
task ci
task dev
```

## First Commands in Telegram

- `/status`
- `/sessions`
- `/run Hello from Telegram`

For failures, see [Testing, Linting, Coverage](testing-quality.md).
