package tui

import "github.com/charmbracelet/lipgloss"

// Palette from the cyberdeck's neon-deck i3 config.
const (
	pink       = "#ff2a6d"
	cyan       = "#05d9e8"
	background = "#0a0a12"
	text       = "#e6e6f0"
	muted      = "#5a5a78"
	yellow     = "#f5b700"
)

var (
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(background)).Background(lipgloss.Color(pink)).Bold(true).Padding(0, 1)
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(cyan)).Bold(true)
	userStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(pink)).Bold(true)
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(muted))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(yellow))
	bodyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(text))
)
