# Opencode Telegram Bot

A lightweight Telegram bot for interfacing with an Opencode server, written in Go.

## Quick Start

### Prerequisites

- Go 1.20+
- A Telegram bot token from [BotFather](https://t.me/botfather)
- A running Opencode server

### Setup

1. Clone this repository and create a `.env` file:

```bash
cp .env.example .env
```

2. Set the required environment variables in `.env`:

```env
TELEGRAM_BOT_TOKEN=your-bot-token-from-botfather
OPENCODE_BASE_URL=http://localhost:4096
ALLOWED_TELEGRAM_IDS=your-telegram-user-id
ADMIN_TELEGRAM_IDS=your-telegram-user-id
```

3. Build and run:

```bash
task build
task dev
```

### Docker

Build and run with Docker:

```bash
task docker:build
task docker:run
```

## Features

- **Telegram Polling**: Receives commands without a public webhook
- **Session Management**: Create, list, and abort Opencode sessions
- **Real-time Streaming**: SSE-based message streaming with debouncing
- **ID-based Access Control**: Restrict bot access to specific Telegram user IDs
- **Minimal Deployment**: ~7.3MB distroless Docker image

## Commands

- `/status` - Show Opencode server status
- `/sessions` - List all sessions
- `/run <prompt>` - Create a session and run a prompt
- `/abort <session_id>` - Abort a session (admin only)

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TELEGRAM_BOT_TOKEN` | ✓ | — | Bot token from BotFather |
| `OPENCODE_BASE_URL` | | `http://localhost:4096` | Opencode server URL |
| `OPENCODE_AUTH_TOKEN` | | — | Optional bearer token for Opencode |
| `ALLOWED_TELEGRAM_IDS` | | — | Comma/space-separated IDs (empty = allow all) |
| `ADMIN_TELEGRAM_IDS` | | — | Comma/space-separated admin IDs |
| `TELEGRAM_MODE` | | `polling` | `polling` or `webhook` (webhook not yet implemented) |
| `PORT` | | `3000` | Listen port (for webhook mode) |
| `REDIS_URL` | | — | Redis connection string (for persistence) |

## Project Structure

Following [golang-standards/project-layout](https://github.com/golang-standards/project-layout):

```
opencode-telegram/
├── cmd/
│   └── opencode-bot/       # Application entrypoint
│       └── main.go
├── internal/
│   └── bot/                # Bot business logic (not importable by external packages)
│       ├── config.go
│       ├── opencode_client.go
│       ├── telegram.go
│       ├── events.go
│       └── debounce.go
├── pkg/
│   └── store/              # Store interface and implementations (reusable)
│       ├── interface.go
│       └── memory.go
├── Dockerfile              # Multi-stage build for minimal image
├── go.mod
├── go.sum
├── .env.example
└── README.md
```

## Development

### Local Build

```bash
go build -o opencode-bot ./cmd/opencode-bot
```

### Run with Task

If you have [Task](https://taskfile.dev/) installed:

```bash
task dev       # Run in polling mode with hot-reload
task build     # Build static binary
task tidy      # Tidy dependencies
task docker    # Build Docker image
```

## Notes

- The bot runs in polling mode by default (long-polling for updates)
- ID-based access control is applied to all commands
- Session → Telegram message mappings are stored in-memory (volatile across restarts)
- Event streaming uses debouncing (500ms) to prevent Telegram rate-limit issues
