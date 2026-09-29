# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

MooFetch: a Go CLI that bulk-downloads files from Moodle-based campuses (UFRO) using an injected session cookie. Docs and README are in Spanish. Module name is `moofetch`; the binary lives in `cmd/moofetch`.

## Commands

```bash
go build -o bin/moofetch ./cmd/moofetch   # build (version vars: -ldflags "-X main.Version=... -X main.Commit=... -X main.BuildDate=...")
go test -race ./...                                # CI runs: go test -v -race -timeout 5m -coverprofile=coverage.out ./...
go test -race -run TestName ./internal/kernel/     # single test in one package
gofmt -l .                                         # CI fails if this prints anything
golangci-lint run                                  # config in .golangci.yml (gocognit>20, gocyclo>15, revive; tests excluded)
./bin/moofetch run --demo -o /tmp/out          # exercise the pipeline without network (demo plugin)
```

## Architecture

Modular monolith with a static microkernel (hexagonal-style). Full diagram in `docs/architecture.md`; decisions in `docs/adr/`.

Dependency direction is strictly inward, no cycles:
`cmd/moofetch` → `internal/tui`, `internal/kernel`, `internal/plugins/*`, `internal/logger`, `internal/auth`; `tui` → `kernel`/`domain`; `kernel` → `domain`/`logger`; `auth` imports neither `kernel` nor `tui` (tui receives its behaviour via injected `AuthOptions`); `plugins/*` → `domain`/`logger` only (plugins must NOT import `kernel` or `tui`).

- `cmd/moofetch` is the composition root: Cobra flags (`root.go`, `run.go`), TTY vs headless detection, plugin wiring, and mapping errors to POSIX exit codes (`exitcode.go`: 0 ok, 1 general, 2 usage, 77 auth, 130 interrupted). Keep exit-code semantics stable; scripts depend on them.
- `internal/domain`: pure types (`Task`, `Result`, `Event`, `ProgressFunc`, `ErrAuthenticationFailed`, `FatalAuthError`). `kernel` re-exports these as type aliases for backward compatibility; new code should import `domain` directly.
- `internal/kernel`: `Registry` (plugins resolved via `CanHandle(url)`), `Dispatch` worker pool bounded by a channel semaphore, and an auth circuit breaker: any fatal auth error cancels the whole batch context (fail-fast). Plugins are injected with `kernel.WithPlugins(...)`; there are no `init()` registrations. The `DownloaderPlugin` interface is defined in the kernel (consumer-driven).
- `internal/plugins/moodle`: authenticated HTTP download, handles 303-to-login as auth failure, inactivity timeout, sanitizes `Content-Disposition` filenames (path traversal), writes to `*.moofetch.part` and renames atomically; partial files are removed on failure/cancel. `plugins/demo` is a deterministic simulator.
- `internal/tui`: `huh` form, splash, Bubble Tea progress card, and `RunHeadlessProgress` (plain progress to **stderr**, keeping stdout clean for pipes). Honors `NO_COLOR`.
- `internal/auth`: browser-cookie autodetection (ADR 003). Catalog of universities (`Universities`, `DomainForUniversity`), `ValidateDomain`, `ResolveDomain`, and `Detect` via an injected `CookieProvider`, returning `MoodleSession=<value>`. Values are never logged or persisted. The `--uni <key>` and `--domain <host>` flags (`cmd/moofetch/auth.go`) select the campus (`--domain` wins); `tui/authstep.go` asks for university and consent in the interactive form and falls back to manual cookie pasting on any failure.
- `internal/logger`: `log/slog` wrapper; cookies are redacted via `slog.LogValuer` (`SessionCookie`). Never log raw cookies; cookies are never persisted to disk.

The kernel and domain must stay free of UI, stdout, and concrete HTTP dependencies; the UI consumes only the event stream (`domain.EventHandler`).
