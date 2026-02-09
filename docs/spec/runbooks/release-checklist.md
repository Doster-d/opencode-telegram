# Release Checklist

Links:
- [Spec index](../spec-index.md)
- [Testing, Linting, Coverage](testing-quality.md)

## Pre-Release Checklist

- [ ] `go build -v ./...` passes
- [ ] `go test -v ./...` passes
- [ ] `golangci-lint` passes
- [ ] coverage gate >= 90% passes
- [ ] `README.md` and docs/spec links are up to date
- [ ] `.env.example` still matches runtime expectations
- [ ] no secrets included in diff

## Recommended Command Bundle

```bash
task ci
```

Or without Task:

```bash
go build -v ./...
go test -v ./...
"$(go env GOPATH)/bin/golangci-lint" run --config .golangci.yml ./...
go test -covermode=count -coverprofile=coverage.out ./internal/... ./pkg/...
go run ./cmd/coveragecheck -file coverage.out -min 90
```

## Post-Release Smoke Checks

- [ ] bot starts cleanly with production env
- [ ] `/status` returns expected endpoint
- [ ] `/run` produces response and edited message stream
- [ ] no unexpected error spikes in logs
