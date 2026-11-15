package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jgarverick/ugen300-tui/internal/ollama"
	"github.com/jgarverick/ugen300-tui/internal/tui"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ugen300-tui:", err)
		os.Exit(1)
	}
}

func run() error {
	endpoint := flag.String("host", env("HAILO_OLLAMA_HOST", "http://localhost:8000"), "Ollama-compatible service URL")
	model := flag.String("model", env("HAILO_OLLAMA_MODEL", ""), "model name (defaults to first available)")
	system := flag.String("system", "", "optional system prompt")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("ugen300-tui", version)
		return nil
	}
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments; use -help for usage")
	}
	client, err := ollama.New(*endpoint)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	_, err = tea.NewProgram(
		tui.New(ctx, client, *endpoint, *model, *system),
		tea.WithAltScreen(), tea.WithContext(ctx),
	).Run()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
