package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jgarverick/ugen300-tui/internal/ollama"
)

const welcome = "Welcome to the neon deck.\n\nEnter a prompt to chat with your Hailo models.\nUse /help for commands. Alt+Enter inserts a newline."

type backend interface {
	Models(context.Context) ([]ollama.Model, error)
	Chat(context.Context, string, []ollama.Message, func(ollama.Event)) error
	Catalog(context.Context) ([]string, error)
	Pull(context.Context, string, func(ollama.PullProgress)) error
	Remove(context.Context, string) error
}

type modelsMsg struct {
	models []ollama.Model
	err    error
}

type streamMsg struct {
	id    int
	event ollama.Event
	err   error
}

type managementMsg struct {
	id        int
	progress  *ollama.PullProgress
	models    []ollama.Model
	catalog   []string
	err       error
	done      bool
	succeeded bool
}

type turn struct {
	role    string
	content string
}

type Model struct {
	ctx        context.Context
	client     backend
	endpoint   string
	model      string
	system     string
	models     []ollama.Model
	history    []ollama.Message
	turns      []turn
	input      textarea.Model
	viewport   viewport.Model
	spinner    spinner.Model
	width      int
	height     int
	loading    bool
	busy       bool
	status     string
	pending    string
	reply      string
	requestID  int
	cancel     context.CancelFunc
	stream     chan streamMsg
	operation  string
	target     string
	management chan managementMsg
}

func New(ctx context.Context, client backend, endpoint, model, system string) Model {
	input := textarea.New()
	input.Placeholder = "Message your deck..."
	input.Prompt = "> "
	input.CharLimit = 0
	input.ShowLineNumbers = false
	input.SetHeight(3)
	input.Focus()
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.FocusedStyle.Text = bodyStyle
	input.FocusedStyle.Prompt = userStyle
	input.BlurredStyle.Text = bodyStyle
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = labelStyle
	v := viewport.New(80, 10)
	m := Model{
		ctx: ctx, client: client, endpoint: endpoint, model: model, system: system,
		input: input, viewport: v, spinner: s, width: 80, height: 24,
		loading: true, status: "Connecting...",
	}
	m.resize()
	m.renderTranscript()
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick, m.loadModels())
}

func (m Model) loadModels() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
		defer cancel()
		models, err := m.client.Models(ctx)
		return modelsMsg{models: models, err: err}
	}
}

func waitStream(ch <-chan streamMsg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		m.renderTranscript()
		return m, nil
	case modelsMsg:
		m.loading = false
		if msg.err != nil {
			m.status = "Connection failed: " + msg.err.Error() + " | /models to retry"
			m.turns = append(m.turns, turn{role: "deck", content: m.status})
		} else {
			m.models = msg.models
			if len(m.models) == 0 {
				m.status = "No models found. Install a model on the service, then /models."
			} else {
				if m.model == "" {
					m.model = m.models[0].Name
				}
				m.status = fmt.Sprintf("Connected | %d models | /help for commands", len(m.models))
				var names []string
				for _, model := range m.models {
					names = append(names, model.Name)
				}
				m.turns = append(m.turns, turn{role: "deck", content: "Available models: " + strings.Join(names, ", ")})
			}
		}
		m.renderTranscript()
		return m, nil
	case managementMsg:
		if msg.id != m.requestID || !m.busy || m.operation == "" {
			return m, nil
		}
		if msg.progress != nil {
			m.status = "Pulling " + m.target + " | " + msg.progress.Status
			if msg.progress.Total > 0 {
				m.status += fmt.Sprintf(" | %.1f%%", 100*float64(msg.progress.Completed)/float64(msg.progress.Total))
			}
		}
		if !msg.done {
			return m, waitManagement(m.management)
		}
		op, target := m.operation, m.target
		m.finishManagement(op + " complete: " + target)
		if op == "Remove" && msg.succeeded && m.model == target {
			m.model = ""
			m.history = nil
			m.models = nil
		}
		if msg.err != nil {
			m.status = op + " failed: " + msg.err.Error() + " | /models to refresh"
		} else if op == "Catalog" {
			m.status = "Download catalog: " + strings.Join(msg.catalog, ", ")
		} else {
			m.models = msg.models
			if m.model == "" && len(m.models) > 0 {
				m.model = m.models[0].Name
			}
		}
		m.turns = append(m.turns, turn{role: "deck", content: m.status})
		m.renderTranscript()
		return m, nil
	case streamMsg:
		if msg.id != m.requestID || !m.busy || m.operation != "" {
			return m, nil
		}
		m.reply += msg.event.Content
		if msg.err != nil {
			m.finish(false, "Request failed: "+msg.err.Error())
		} else if msg.event.Done {
			status := "Ready"
			if msg.event.Tokens > 0 {
				status = fmt.Sprintf("Ready | %d tokens", msg.event.Tokens)
				if msg.event.EvalDuration > 0 {
					status += fmt.Sprintf(" | %.1f tok/s", float64(msg.event.Tokens)/msg.event.EvalDuration.Seconds())
				}
			}
			m.finish(true, status)
		}
		m.renderTranscript()
		if m.busy {
			return m, waitStream(m.stream)
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.busy {
				m.stop()
				m.renderTranscript()
				return m, nil
			}
			return m, tea.Quit
		case "ctrl+q":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "esc":
			if m.busy {
				m.stop()
				m.renderTranscript()
			}
			return m, nil
		case "pgup", "ctrl+u":
			m.viewport.HalfPageUp()
			return m, nil
		case "pgdown", "ctrl+d":
			m.viewport.HalfPageDown()
			return m, nil
		case "ctrl+n":
			if !m.busy && len(m.models) > 0 {
				next := 0
				for i, model := range m.models {
					if model.Name == m.model {
						next = (i + 1) % len(m.models)
						break
					}
				}
				m.setModel(m.models[next].Name)
			}
			return m, nil
		case "enter":
			if m.busy {
				return m, nil
			}
			return m.submit()
		case "alt+enter":
			m.input.InsertString("\n")
			return m, nil
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	var inputCmd, viewCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	if _, ok := msg.(tea.MouseMsg); ok {
		m.viewport, viewCmd = m.viewport.Update(msg)
	}
	return m, tea.Batch(inputCmd, viewCmd)
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	prompt := strings.TrimSpace(m.input.Value())
	if prompt == "" {
		return m, nil
	}
	if strings.HasPrefix(prompt, "/") {
		m.input.Reset()
		return m.command(prompt)
	}
	if m.model == "" {
		m.status = "Select a model with /model NAME, or retry discovery with /models."
		return m, nil
	}
	m.input.Reset()
	m.pending, m.reply = prompt, ""
	m.turns = append(m.turns, turn{role: "you", content: prompt})
	m.busy = true
	m.status = "Generating... | Esc or Ctrl+C to stop"
	m.requestID++
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	ch := make(chan streamMsg)
	m.stream = ch
	id := m.requestID
	messages := make([]ollama.Message, 0, len(m.history)+2)
	if m.system != "" {
		messages = append(messages, ollama.Message{Role: "system", Content: m.system})
	}
	messages = append(messages, m.history...)
	messages = append(messages, ollama.Message{Role: "user", Content: prompt})
	client, model := m.client, m.model
	start := func() tea.Msg {
		err := client.Chat(ctx, model, messages, func(event ollama.Event) {
			select {
			case ch <- streamMsg{id: id, event: event}:
			case <-ctx.Done():
			}
		})
		if err != nil {
			select {
			case ch <- streamMsg{id: id, err: err}:
			case <-ctx.Done():
			}
		}
		// Wake any outstanding receiver even when a request was cancelled.
		close(ch)
		return nil
	}
	m.renderTranscript()
	m.viewport.GotoBottom()
	return m, tea.Batch(start, waitStream(ch))
}

func (m *Model) finish(success bool, status string) {
	m.cancel()
	m.cancel = nil
	if success {
		m.history = append(m.history,
			ollama.Message{Role: "user", Content: m.pending},
			ollama.Message{Role: "assistant", Content: m.reply},
		)
		m.turns = append(m.turns, turn{role: m.model, content: m.reply})
	} else {
		m.turns = append(m.turns, turn{role: m.model + " (incomplete)", content: m.reply})
		m.turns = append(m.turns, turn{role: "deck", content: status})
	}
	m.busy, m.pending, m.reply = false, "", ""
	m.status = status
}

func (m *Model) setModel(name string) {
	m.model = name
	m.history, m.turns = nil, nil
	m.status = "Model: " + name + " | New conversation"
	m.renderTranscript()
}

func (m Model) command(prompt string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(prompt)
	switch fields[0] {
	case "/quit", "/exit":
		return m, tea.Quit
	case "/help":
		m.turns = append(m.turns, turn{role: "deck", content: "/help          Show this help\n/models        Refresh and list models\n/model NAME    Switch model (clears conversation)\n/clear         Clear conversation\n/quit          Exit\n\nEnter send | Alt+Enter newline | Ctrl+N next model\nEsc/Ctrl+C stop generation | Ctrl+Q quit\nPgUp/PgDn or Ctrl+U/Ctrl+D scroll\n\nOnly completed exchanges are sent as context. Chats stay in memory;\nno file access, shell commands, or automatic disk storage."})
		m.turns[len(m.turns)-1].content += "\n\n/catalog       List Hailo models available for download\n/pull NAME     Download a model onto the service\n/remove NAME --confirm   Delete a model from service storage\n\nEsc cancels the client request, not necessarily the server operation.\nUse /models to check installed models after cancellation."
	case "/clear":
		m.history, m.turns = nil, nil
		m.status = "Conversation cleared"
	case "/models":
		if m.loading {
			m.status = "Model discovery is already in progress"
			return m, nil
		}
		m.loading = true
		m.status = "Refreshing models..."
		m.renderTranscript()
		return m, m.loadModels()
	case "/catalog", "/pull", "/remove":
		if m.loading {
			m.status = "Wait for model discovery to finish"
		} else if fields[0] == "/catalog" && len(fields) == 1 {
			return m.startManagement("Catalog", "")
		} else if fields[0] == "/pull" && len(fields) == 2 {
			return m.startManagement("Pull", fields[1])
		} else if fields[0] == "/remove" && len(fields) == 3 && fields[2] == "--confirm" {
			return m.startManagement("Remove", fields[1])
		} else {
			m.status = "Usage: /catalog | /pull NAME | /remove NAME --confirm (deletes from service storage)"
		}
	case "/model":
		if len(fields) != 2 {
			m.status = "Usage: /model NAME"
		} else {
			m.setModel(fields[1])
		}
	default:
		m.status = "Unknown command. Use /help."
	}
	m.renderTranscript()
	m.viewport.GotoBottom()
	return m, nil
}

func waitManagement(ch <-chan managementMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (m Model) startManagement(operation, target string) (tea.Model, tea.Cmd) {
	m.busy, m.operation, m.target = true, operation, target
	m.requestID++
	m.status = operation + " in progress: " + target + " | Esc to cancel client request"
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	ch := make(chan managementMsg)
	m.management = ch
	id, client := m.requestID, m.client
	start := func() tea.Msg {
		defer close(ch)
		defer cancel()
		result := managementMsg{id: id, done: true}
		switch operation {
		case "Catalog":
			catalogCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			result.catalog, result.err = client.Catalog(catalogCtx)
			stop()
		case "Pull":
			result.err = client.Pull(ctx, target, func(progress ollama.PullProgress) {
				select {
				case ch <- managementMsg{id: id, progress: &progress}:
				case <-ctx.Done():
				}
			})
		case "Remove":
			result.err = client.Remove(ctx, target)
		}
		result.succeeded = result.err == nil
		if result.err == nil && operation != "Catalog" {
			listCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			result.models, result.err = client.Models(listCtx)
			if result.err != nil {
				result.err = fmt.Errorf("operation completed, but model refresh failed: %w", result.err)
			}
			stop()
		}
		select {
		case ch <- result:
		case <-ctx.Done():
		}
		return nil
	}
	m.renderTranscript()
	return m, tea.Batch(start, waitManagement(ch))
}

func (m *Model) finishManagement(status string) {
	m.cancel()
	m.cancel = nil
	m.busy, m.operation, m.target = false, "", ""
	m.status = status
}

func (m *Model) stop() {
	if m.operation == "" {
		m.finish(false, "Stopped. Partial response is not included in chat context.")
		return
	}
	m.finishManagement("Client request stopped. Server operation may continue; /models to check.")
	m.turns = append(m.turns, turn{role: "deck", content: m.status})
}

func (m *Model) resize() {
	w := max(1, m.width-4)
	m.input.SetWidth(w)
	m.viewport.Width = w
	m.viewport.Height = max(1, m.height-lipgloss.Height(m.header())-lipgloss.Height(m.composer())-lipgloss.Height(m.footer())-2)
}

func (m *Model) renderTranscript() {
	m.resize()
	atBottom := m.viewport.AtBottom()
	w := max(1, m.viewport.Width)
	var b strings.Builder
	if len(m.turns) == 0 {
		b.WriteString(bodyStyle.Width(w).Render(welcome))
	}
	for _, turn := range m.turns {
		style := labelStyle
		if turn.role == "you" {
			style = userStyle
		}
		b.WriteString(style.Width(w).Render(ansi.Strip(turn.role)))
		b.WriteByte('\n')
		b.WriteString(bodyStyle.Width(w).Render(ansi.Strip(turn.content)))
		b.WriteString("\n\n")
	}
	if m.busy && m.operation == "" {
		b.WriteString(labelStyle.Width(w).Render(ansi.Strip(m.model)))
		b.WriteByte('\n')
		b.WriteString(bodyStyle.Width(w).Render(ansi.Strip(m.reply)))
	}
	m.viewport.SetContent(b.String())
	if atBottom {
		m.viewport.GotoBottom()
	}
}

func (m Model) header() string {
	w := max(1, m.width-4)
	name := m.model
	if name == "" {
		name = "no model selected"
	}
	return titleStyle.Render("UGEN300 // NEON DECK") + "\n" +
		labelStyle.Render(ansi.Truncate(ansi.Strip(name), w, "...")) + "\n" +
		dimStyle.Render(ansi.Truncate(ansi.Strip(m.endpoint), w, "..."))
}

func (m Model) composer() string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(pink)).Render(m.input.View())
}

func (m Model) footer() string {
	status := m.status
	if m.busy || m.loading {
		status = m.spinner.View() + " " + ansi.Strip(status)
	} else {
		status = ansi.Strip(status)
	}
	w := max(1, m.width-4)
	return warnStyle.Render(ansi.Truncate(status, w, "...")) + "\n" +
		dimStyle.Render(ansi.Truncate("Enter send | Alt+Enter newline | Ctrl+N model | /help | Ctrl+Q quit", w, "..."))
}

func (m Model) View() string {
	if m.width < 24 || m.height < 12 {
		return warnStyle.Render("Terminal too small.\nResize or Ctrl+Q to quit.")
	}
	m.resize()
	content := lipgloss.JoinVertical(lipgloss.Left,
		m.header(), "", m.viewport.View(), m.composer(), m.footer(),
	)
	return lipgloss.NewStyle().Background(lipgloss.Color(background)).
		Foreground(lipgloss.Color(text)).Padding(0, 1).
		Width(m.width).Height(m.height).Render(content)
}
