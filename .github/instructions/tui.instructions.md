---
applyTo: "internal/tui/**/*.go"
---

# TUI instructions

- Follow Bubble Tea's model/update/view pattern. Execute network work in commands
  and deliver results as messages; do not mutate UI state from worker goroutines.
- Use request IDs to ignore stale stream events after cancellation or a new
  request. Cancellation must release workers and outstanding channel receivers.
- Include only completed user/assistant pairs in conversation context. Keep
  partial responses visible as incomplete, without sending them in later chats.
- Preserve draft input while streaming. Sending and model switching are disabled
  during generation and model management; cancellation and quitting remain
  available. Serialize management with initial discovery.
- Keep `/models` (installed) distinct from `/catalog` (downloadable).
  `/pull NAME` shows progress; `/remove NAME --confirm` requires explicit
  confirmation and deletes from server storage. Refresh installed models after
  successful mutations and reset context when removing the selected model.
- Management operations must not add user/assistant history entries. Warn that
  cancelling the client request may not stop server-side changes.
- Model selection starts a fresh conversation. Keep help text and README key
  bindings consistent with command and key handling.
- Keep transcript navigation separate from text editing: typing characters such
  as `j` must not activate viewport scrolling.
- Keep layout within terminal dimensions and retain the small-terminal fallback.
  Respect user scroll position while streaming unless already at the bottom.
- Use the shared theme in `theme.go`: neon pink `#ff2a6d`, cyan `#05d9e8`,
  background `#0a0a12`, text `#e6e6f0`, muted `#5a5a78`, and amber `#f5b700`.
  These colors are compiled in; never read a user's i3 config at runtime.
- Strip terminal escape sequences from service-supplied content before rendering.
  Preserve raw message content for protocol context.
- Prefer model-update tests with the fake backend for state transitions,
  cancellation, history, command behavior, and terminal sizing.
