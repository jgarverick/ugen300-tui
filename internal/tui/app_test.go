package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jgarverick/ugen300-tui/internal/ollama"
)

type fakeBackend struct {
	chat   func(context.Context, string, []ollama.Message, func(ollama.Event)) error
	pull   func(context.Context, string, func(ollama.PullProgress)) error
	remove func(context.Context, string) error
}

func (f fakeBackend) Catalog(context.Context) ([]string, error) {
	return []string{"one", "two", "downloadable"}, nil
}

func (f fakeBackend) Pull(ctx context.Context, model string, emit func(ollama.PullProgress)) error {
	return f.pull(ctx, model, emit)
}

func (f fakeBackend) Remove(ctx context.Context, model string) error {
	return f.remove(ctx, model)
}

func (f fakeBackend) Models(context.Context) ([]ollama.Model, error) {
	return []ollama.Model{{Name: "one"}, {Name: "two"}}, nil
}

func (f fakeBackend) Chat(ctx context.Context, model string, messages []ollama.Message, emit func(ollama.Event)) error {
	return f.chat(ctx, model, messages, emit)
}

func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestStreamingConversation(t *testing.T) {
	calls := 0
	client := fakeBackend{chat: func(ctx context.Context, model string, messages []ollama.Message, emit func(ollama.Event)) error {
		calls++
		if model != "one" || messages[0].Role != "system" || messages[0].Content != "system prompt" {
			t.Errorf("unexpected chat request: %s %+v", model, messages)
		}
		if calls == 2 && (len(messages) != 4 || messages[2].Content != "Hello!") {
			t.Errorf("missing conversation context: %+v", messages)
		}
		emit(ollama.Event{Content: "Hello"})
		emit(ollama.Event{Content: "!", Done: true, Tokens: 2})
		return nil
	}}
	m := New(context.Background(), client, "http://deck:8000", "one", "system prompt")
	for _, prompt := range []string{"Hi", "Again"} {
		m.input.SetValue(prompt)
		next, cmd := m.submit()
		m = next.(Model)
		batch := cmd().(tea.BatchMsg)
		done := make(chan struct{})
		go func() {
			batch[0]()
			close(done)
		}()
		for m.busy {
			m = update(m, batch[1]())
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("stream worker did not finish")
		}
	}
	if len(m.history) != 4 || m.busy || m.history[3].Content != "Hello!" {
		t.Fatalf("unexpected conversation: %+v", m.history)
	}
}

func TestCancelAndStaleEvents(t *testing.T) {
	m := New(context.Background(), fakeBackend{}, "http://deck:8000", "one", "")
	m.input.SetValue("first")
	next, _ := m.submit()
	m = next.(Model)
	id := m.requestID
	m = update(m, streamMsg{id: id, event: ollama.Event{Content: "partial"}})
	m = update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.busy || len(m.history) != 0 || !strings.Contains(m.turns[1].role, "incomplete") {
		t.Fatal("cancelled response was included in conversation context")
	}
	m.input.SetValue("second")
	next, _ = m.submit()
	m = next.(Model)
	m = update(m, streamMsg{id: id, event: ollama.Event{Content: "stale", Done: true}})
	if m.reply != "" || !m.busy {
		t.Fatal("stale event affected new request")
	}
	m = update(m, streamMsg{id: m.requestID, err: errors.New("offline")})
	if m.busy || len(m.history) != 0 || !strings.Contains(m.status, "offline") {
		t.Fatal("failed stream was not handled")
	}
}

func TestModelsAndCommands(t *testing.T) {
	m := New(context.Background(), fakeBackend{}, "http://deck:8000", "", "")
	m = update(m, modelsMsg{models: []ollama.Model{{Name: "one"}, {Name: "two"}}})
	if m.model != "one" || m.loading || !strings.Contains(m.turns[0].content, "two") {
		t.Fatal("model discovery failed")
	}
	m.history = []ollama.Message{{Role: "user", Content: "old"}}
	m = update(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	if m.model != "two" || len(m.history) != 0 || len(m.turns) != 0 {
		t.Fatal("model switch did not clear conversation")
	}
	m.input.SetValue("/model custom")
	m = update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.model != "custom" {
		t.Fatal("explicit model selection failed")
	}
	m.history = []ollama.Message{{Role: "user", Content: "old"}}
	m.input.SetValue("/model custom")
	m = update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.history) != 0 {
		t.Fatal("selecting the same model did not start a fresh conversation")
	}
	m.input.SetValue("/help")
	m = update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.turns) != 1 || !strings.Contains(m.turns[0].content, "/clear") {
		t.Fatal("help was not shown")
	}
	m.input.SetValue("/clear")
	m = update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.turns) != 0 {
		t.Fatal("clear did not reset transcript")
	}
}

func TestDisconnectedPromptAndLayout(t *testing.T) {
	m := New(context.Background(), fakeBackend{}, "http://deck:8000", "", "")
	m = update(m, modelsMsg{err: errors.New("offline")})
	m.input.SetValue("keep this draft")
	m = update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.input.Value() != "keep this draft" || m.busy {
		t.Fatal("draft lost without model")
	}

	m.input.Reset()
	m.status = "Ready"
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 40, Height: 18}, {Width: 120, Height: 40}, {Width: 24, Height: 12}} {
		m = update(m, size)
		view := m.View()
		if lipgloss.Width(view) > size.Width || lipgloss.Height(view) > size.Height {
			t.Errorf("view %dx%d exceeds terminal %dx%d", lipgloss.Width(view), lipgloss.Height(view), size.Width, size.Height)
		}
	}
	m = update(m, tea.WindowSizeMsg{Width: 10, Height: 5})
	if !strings.Contains(m.View(), "too small") {
		t.Fatal("missing small-terminal fallback")
	}
}

func TestTypingDoesNotScrollTranscript(t *testing.T) {
	m := New(context.Background(), fakeBackend{}, "http://deck:8000", "one", "")
	m.turns = []turn{{role: "one", content: strings.Repeat("line\n", 100)}}
	m.renderTranscript()
	m.viewport.GotoTop()
	m = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.input.Value() != "j" || m.viewport.YOffset != 0 {
		t.Fatal("typing changed transcript scroll position")
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.viewport.YOffset == 0 {
		t.Fatal("page-down did not scroll")
	}
}

func TestCancelledStreamWorkerExits(t *testing.T) {
	started := make(chan struct{})
	client := fakeBackend{chat: func(ctx context.Context, _ string, _ []ollama.Message, emit func(ollama.Event)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	m := New(context.Background(), client, "http://deck:8000", "one", "")
	m.input.SetValue("Hi")
	next, cmd := m.submit()
	m = next.(Model)
	batch := cmd().(tea.BatchMsg)
	done := make(chan struct{})
	go func() {
		batch[0]()
		close(done)
	}()
	<-started
	m = update(m, tea.KeyMsg{Type: tea.KeyEsc})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled stream worker leaked")
	}

	if event := batch[1]().(streamMsg); event.id != 0 {
		t.Fatal("closed channel did not release outstanding receiver")
	}
}

func runManagement(t *testing.T, m Model, prompt string) Model {
	t.Helper()
	m.input.SetValue(prompt)
	next, cmd := m.submit()
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("command %q did not start", prompt)
	}
	batch := cmd().(tea.BatchMsg)
	done := make(chan struct{})
	go func() {
		batch[0]()
		close(done)
	}()
	for m.busy {
		m = update(m, batch[1]())
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("management worker did not finish")
	}
	return m
}

func TestModelManagementCommands(t *testing.T) {
	removed := false
	client := fakeBackend{
		pull: func(ctx context.Context, model string, emit func(ollama.PullProgress)) error {
			if model != "two" {
				t.Errorf("pull model = %s", model)
			}
			emit(ollama.PullProgress{Status: "downloading", Completed: 50, Total: 100})
			emit(ollama.PullProgress{Status: "success"})
			return nil
		},
		remove: func(ctx context.Context, model string) error {
			removed = true
			if model != "one" {
				t.Errorf("remove model = %s", model)
			}
			return nil
		},
	}
	m := New(context.Background(), client, "http://deck:8000", "one", "")
	m.loading = false
	m.history = []ollama.Message{{Role: "user", Content: "keep context"}}
	m = runManagement(t, m, "/catalog")
	if !strings.Contains(m.status, "downloadable") || len(m.history) != 1 {
		t.Fatal("catalog changed context or omitted available models")
	}
	m = runManagement(t, m, "/pull two")
	if len(m.models) != 2 || len(m.history) != 1 || m.operation != "" {
		t.Fatal("pull did not refresh models or altered conversation")
	}
	m.input.SetValue("/remove one")
	next, cmd := m.submit()
	m = next.(Model)
	if cmd != nil || removed || m.busy || !strings.Contains(m.status, "--confirm") {
		t.Fatal("removal did not require confirmation")
	}
	m = runManagement(t, m, "/remove one --confirm")
	if !removed || len(m.history) != 0 {
		t.Fatal("confirmed removal did not clear deleted model context")
	}
}

func TestManagementCancellationAndFailure(t *testing.T) {
	m := New(context.Background(), fakeBackend{}, "http://deck:8000", "one", "")
	m.loading = false
	next, _ := m.startManagement("Pull", "two")
	m = next.(Model)
	id := m.requestID
	m.history = []ollama.Message{{Role: "user", Content: "keep"}}
	m = update(m, managementMsg{id: id, progress: &ollama.PullProgress{Status: "downloading", Completed: 50, Total: 100}})
	if !strings.Contains(m.status, "50.0%") {
		t.Fatal("missing pull progress")
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.busy || len(m.history) != 1 || !strings.Contains(m.status, "may continue") {
		t.Fatal("management cancellation lost context or omitted server caveat")
	}
	next, _ = m.startManagement("Remove", "one")
	m = next.(Model)
	m = update(m, managementMsg{id: id, done: true})
	if !m.busy {
		t.Fatal("stale management event affected new operation")
	}
	m = update(m, managementMsg{id: m.requestID, done: true, err: errors.New("offline")})
	if m.model != "one" || len(m.history) != 1 || !strings.Contains(m.status, "offline") {
		t.Fatal("failed removal changed model or history")
	}
	next, _ = m.startManagement("Remove", "one")
	m = next.(Model)
	m = update(m, managementMsg{id: m.requestID, done: true, succeeded: true, err: errors.New("operation completed, but model refresh failed")})
	if m.model != "" || len(m.history) != 0 {
		t.Fatal("successful deletion with failed refresh kept deleted model context")
	}
}

func TestCancelledManagementWorkerExits(t *testing.T) {
	started := make(chan struct{})
	client := fakeBackend{pull: func(ctx context.Context, _ string, emit func(ollama.PullProgress)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	m := New(context.Background(), client, "http://deck:8000", "one", "")
	m.loading = false
	next, cmd := m.startManagement("Pull", "two")
	m = next.(Model)
	batch := cmd().(tea.BatchMsg)
	done := make(chan struct{})
	go func() {
		batch[0]()
		close(done)
	}()
	<-started
	m = update(m, tea.KeyMsg{Type: tea.KeyEsc})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled management worker leaked")
	}
	if event := batch[1]().(managementMsg); event.id != 0 {
		t.Fatal("closed management channel did not release receiver")
	}
}
