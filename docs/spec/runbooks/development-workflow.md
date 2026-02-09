# Go Development Workflow

Links:
- [Spec index](../spec-index.md)
- [Testing, Linting, Coverage](testing-quality.md)

## Where to Add Code

- Entrypoint and wiring: `cmd/opencode-bot/main.go`
- Bot behavior and handlers: `internal/bot`
- Session storage abstraction: `pkg/store`

## Recommended Flow

1. Add/adjust a unit test first.
2. Implement minimal change.
3. Run local quality checks.
4. Submit PR.

## Quality Command Set

```bash
go build -v ./...
go test -v ./...
"$(go env GOPATH)/bin/golangci-lint" run --config .golangci.yml ./...
go test -covermode=count -coverprofile=coverage.out ./internal/... ./pkg/...
go run ./cmd/coveragecheck -file coverage.out -min 90
```

Or use:

```bash
task ci
```

## Coding Guidelines

- Keep `cmd` thin; move logic into `internal/bot`.
- Preserve test seams via interfaces (`TelegramBotInterface`, `OpencodeClientInterface`, `store.Store`).
- Prefer deterministic tests (bounded waits, no long sleeps).
- Keep diffs small and behavior-focused.
