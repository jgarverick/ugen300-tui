---
applyTo: "internal/ollama/**/*.go"
---

# Ollama client instructions

- Preserve compatibility with `GET /api/tags` and streaming `POST /api/chat`.
  Chat requests include the model, ordered messages, and `stream: true`.
- Use the upstream Hailo Model Zoo GenAI `docs/USAGE.rst`, controller mappings,
  and DTOs as the protocol authority, not generic Ollama assumptions or search
  summaries. UGen300 is hardware; hailo-ollama exposes the HTTP API on port 8000.
- Installed models: `GET /api/tags` returns `models` as objects with `name`.
  Download catalog: `GET /hailo/v1/list` returns `models` as strings.
  Never substitute `/api/generate/hailo/v1/...` routes.
- Pull: `POST /api/pull` with `model` and `stream: true`; parse NDJSON
  `status`, `total`, and `completed`. Require `status: "success"` rather than
  chat's `done: true` or progress reaching 100%.
- Remove: `DELETE /api/delete` with a JSON `model`; accept HTTP 200 with an empty
  body. Failures may contain `code` and `error`.
- Only catalog HEF models run on Hailo hardware; do not imply arbitrary Ollama
  GGUF models are compatible. `/api/generate` is for single-prompt generation,
  not model management.
- Parse newline-delimited JSON incrementally. Handle blank lines, large chunks,
  HTTP failures, service error objects, malformed data, and incomplete streams.
  A successful exchange requires a final `done: true` event.
- Carry content from the final event as well as preceding events. Token counts
  and evaluation duration are optional metadata.
- Honor caller context cancellation throughout connection and streaming.
  Always close response bodies and bound error-body/model-list reads.
- Keep timeouts appropriate for local model startup and long generation; do not
  impose a short total HTTP timeout on streaming responses.
- Preserve base-path endpoints and reject URL credentials, queries, fragments,
  and schemes other than HTTP/HTTPS.
- Test protocol behavior with `httptest`, including payloads, cancellation,
  malformed/truncated streams, and non-success status codes. Avoid live-service
  dependencies and unbounded waits in tests.
- Never pull/delete live cyberdeck models as part of automated validation.
  Cancelling an HTTP request does not guarantee the server stops its operation.
