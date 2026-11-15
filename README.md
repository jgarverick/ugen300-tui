# UGEN300 // Neon Deck

A streaming chat TUI for the cyberdeck's **hailo-ollama** service. Written in Go
with Bubble Tea; builds to standalone executables for macOS, Windows, and Linux
(ARM64 and AMD64). No Go runtime is needed on the target machine.

The neon pink `#ff2a6d`, cyan `#05d9e8`, dark `#0a0a12`, pale text `#e6e6f0`,
and amber `#f5b700` palette matches the cyberdeck's neon-deck i3 theme. Colors are
compiled in, so i3 and its configuration are not required on other platforms.
A true-color terminal gives the best results (Alacritty, Windows Terminal,
iTerm2, etc.).

## Build and run

Install [Go](https://go.dev/dl/) **1.24 or newer**, then:

```sh
go build -o ugen300-tui .
./ugen300-tui -host http://localhost:8000
```

On Windows:

```powershell
go build -o ugen300-tui.exe .
.\ugen300-tui.exe -host http://localhost:8000
```

`localhost` means the machine running this client. From another computer, use
your cyberdeck's hostname/IP or an SSH tunnel:

```sh
ssh -N -L 8000:localhost:8000 user@cyberdeck
# In another terminal:
./ugen300-tui -host http://localhost:8000 -model llama3.2:1b
```

The client uses the [official Hailo-Ollama REST API](https://github.com/hailo-ai/hailo_model_zoo_genai/blob/main/docs/USAGE.rst).
Default host: `http://localhost:8000`.
The first available model is selected unless `-model` is supplied.

| Action | Endpoint | Request / response |
| --- | --- | --- |
| Chat | `POST /api/chat` | `model`, ordered `messages`, `stream: true`; NDJSON ending with `done: true` |
| Installed models | `GET /api/tags` | `models` array of objects with `name` |
| Download catalog | `GET /hailo/v1/list` | `models` array of strings |
| Pull model | `POST /api/pull` | `model`, `stream: true`; NDJSON progress ending with `status: "success"` |
| Remove model | `DELETE /api/delete` | `model`; HTTP 200 with an empty body |

UGen300 is the accelerator hardware; the client talks to the hailo-ollama service,
not directly to the USB device. Models must be Hailo-compatible HEFs from the
service's catalog, not arbitrary Ollama GGUF models. `/api/generate` is the
service's single-prompt endpoint; this client uses `/api/chat` for conversations.
Do not use `/api/generate/hailo/v1/...` paths for model management.

| Option | Purpose |
| --- | --- |
| `-host URL` | Service endpoint; overrides `HAILO_OLLAMA_HOST` |
| `-model NAME` | Initial model; overrides `HAILO_OLLAMA_MODEL` |
| `-system TEXT` | Optional system prompt for each conversation |
| `-version` | Print build version |
| `-help` | Show CLI options |

## Chat

| Key or command | Action |
| --- | --- |
| Enter | Send a prompt |
| Alt+Enter | Insert a newline |
| Esc / Ctrl+C | Stop generation; Ctrl+C exits when idle |
| Ctrl+Q | Exit immediately |
| PgUp / PgDn or Ctrl+U / Ctrl+D | Scroll the transcript |
| Ctrl+N | Cycle available models, starting a fresh conversation |
| `/models` | Refresh and list available models; retry a failed connection |
| `/catalog` | List Hailo models available for download |
| `/pull NAME` | Download a model onto the service, with progress |
| `/remove NAME --confirm` | Delete a model from the service's storage |
| `/model NAME` | Select a model and clear the conversation |
| `/clear` | Clear the conversation |
| `/help` | Show in-app help |
| `/quit` or `/exit` | Exit |

You can compose the next prompt while a response streams. Sending and changing
models are disabled during generation or model management. Model-management
requests wait for initial discovery to finish; the service supports limited
concurrent requests. Pull/removal refresh the installed model list on success.
Deleting the selected model resets chat context and selects another installed
model if one is available.

Pull and removal affect **the server's storage**, not the client computer.
Removal requires the literal `--confirm` argument. Esc/Ctrl+C cancels the client
request, but the service may continue downloading or deleting; use `/models` to
check afterward. No model downloads or deletions occur automatically.

Only completed exchanges become model
context; stopped or failed responses remain visible but are excluded from
future requests. Token counts and throughput appear when supplied by the service.

This is **chat-only**, not a coding agent: it cannot read/edit files or run shell
commands. Chats are kept in memory and are not saved on exit. Conversation context
is sent to the configured service with every prompt; use `/clear` to reset it.
Large conversations may exceed the model's context window; no automatic
truncation or summarization is performed.

Use a trusted network, HTTPS, or an SSH tunnel when connecting remotely. The
client has no authentication options; do not expose an unauthenticated service
to the internet. Host URLs must not contain credentials, queries, or fragments.

## Development and cross-platform builds

```sh
make test
make build
make release VERSION=0.1.0
```

`make release` produces six binaries in `dist/` with `CGO_ENABLED=0`; Make and a
POSIX shell are needed only for this convenience target. You can also
cross-compile directly:

```sh
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o dist/ugen300-tui-darwin-arm64 .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/ugen300-tui-windows-amd64.exe .
```

Protocol contracts were checked against upstream
[`e4ab0ff`](https://github.com/hailo-ai/hailo_model_zoo_genai/tree/e4ab0ff8d9fb08735f1c71ab80ba691ed5b38a91)
(`controller.hpp`, `DTOs.hpp`, and pull/delete implementations), plus the
cyberdeck's live `/api/chat`, `/api/tags`, and `/hailo/v1/list` responses.
Pull/delete tests use mock HTTP servers to avoid modifying installed models.

GitHub Actions tests on all three operating systems and uploads the six
cross-compiled binaries. Downloaded artifacts may need `chmod +x` on Unix.
