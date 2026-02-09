# Testing, Linting, Coverage

Links:
- [Spec index](../spec-index.md)
- [Local Development Runbook](local-dev.md)

## Quality Gates

- Build: `go build -v ./...`
- Tests: `go test -v ./...`
- Lint: `golangci-lint` with `.golangci.yml`
- Coverage threshold: >= 90% for `internal/...` + `pkg/...`

## Commands

```bash
go build -v ./...
go test -v ./...
"$(go env GOPATH)/bin/golangci-lint" run --config .golangci.yml ./...
go test -covermode=count -coverprofile=coverage.out ./internal/... ./pkg/...
go run ./cmd/coveragecheck -file coverage.out -min 90
```

Task shortcut:

```bash
task ci
```

## Troubleshooting

- `golangci-lint: command not found`
  - `go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8`
- `coveragecheck: threshold not met`
  - Add tests for uncovered paths in `internal/bot`
- compile error in tests
  - Re-run `go test ./internal/bot` and inspect malformed blocks in `*_test.go`

## CI Reference

- CI workflow: [`.github/workflows/go.yml`](../../../.github/workflows/go.yml)
