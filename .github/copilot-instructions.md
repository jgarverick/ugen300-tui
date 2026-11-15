# Repository instructions

## Purpose and scope

UGEN300 // Neon Deck is a cross-platform, streaming chat TUI for an
Ollama-compatible hailo-ollama service. It is a chat client, not a coding agent.
Do not add file editing, shell execution, automatic chat persistence, or other
agent capabilities without an explicit request.

## Architecture

- `main.go`: CLI flags, environment defaults, application lifecycle, and version.
- `internal/ollama`: installed/downloadable models, cancellable NDJSON chat/pull
  streaming, and model removal.
- `internal/tui`: Bubble Tea state, input, transcript, commands, and theme.
- `Makefile`: tests, native build, and six cross-platform release binaries.
- `.github/workflows/build.yml`: native OS tests and cross-compiled artifacts.

Keep changes small and follow existing Go patterns. Prefer the standard library
and existing Charm dependencies over new abstractions or packages. Keep network
work outside the Bubble Tea update loop.

## Compatibility and behavior

- Support macOS, Windows, and Linux on AMD64 and ARM64.
- Preserve release builds with `CGO_ENABLED=0`; do not require i3, a shell, or
  platform-specific tools at runtime.
- Default to `http://localhost:8000`. Preserve CLI precedence over
  `HAILO_OLLAMA_HOST` and `HAILO_OLLAMA_MODEL`.
- Keep chats in memory. Only completed exchanges become future model context.
- Follow Hailo's documented API: `/api/chat`, `/api/tags`, `/hailo/v1/list`,
  `/api/pull`, and `DELETE /api/delete`. See scoped Ollama instructions for
  payload and completion contracts. Model mutations affect server storage;
  removal requires explicit confirmation and live tests must not mutate models.
- Treat service responses as untrusted; do not expose terminal control sequences
  in rendered output or add credentials to source code or fixtures.
- Update `README.md` when commands, options, build requirements, or UX change.

## Validation

Use the Go version requirement in `go.mod`. Format Go changes with
`gofmt -w main.go internal`, then run `make test` (tests, vet, and formatting).
Add focused tests for changed behavior in the existing test files.

Run `make build` for executable changes. Run `make release VERSION=dev` when
changing dependencies, platform compatibility, or build configuration.
Outputs belong in the ignored `dist/` directory.

CI additionally runs `go test -race -timeout 60s ./...` on all three operating
systems. Some ARM64 hosts cannot run the race detector because of their VMA
layout; report that limitation and run normal tests rather than claiming race
validation passed.

Tests should use fake backends or `httptest`, not require a live cyberdeck.
Live service checks are optional integration checks and must not replace tests.
