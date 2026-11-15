package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/ollama/api/tags" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"models":[{"name":"deck-model"}]}`)
	}))
	defer server.Close()
	client, err := New(server.URL + "/ollama/")
	if err != nil {
		t.Fatal(err)
	}
	models, err := client.Models(context.Background())
	if err != nil || len(models) != 1 || models[0].Name != "deck-model" {
		t.Fatalf("models = %v, err = %v", models, err)
	}
}

func TestChatStreaming(t *testing.T) {
	large := strings.Repeat("x", 70_000)
	messages := []Message{{Role: "system", Content: "Be brief"}, {Role: "user", Content: "Hello"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Model    string
			Messages []Message
			Stream   bool
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "deck-model" || !body.Stream || fmt.Sprint(body.Messages) != fmt.Sprint(messages) {
			t.Errorf("unexpected body: %+v", body)
		}
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Hello "},"done":false}`)
		w.(http.Flusher).Flush()
		fmt.Fprintf(w, "\n{\"message\":{\"content\":%q},\"done\":false}\n", large)
		fmt.Fprintln(w, `{"message":{"content":"!"},"done":true,"eval_count":12,"eval_duration":2000000000}`)
	}))
	defer server.Close()
	client, _ := New(server.URL)
	var events []Event
	err := client.Chat(context.Background(), "deck-model", messages, func(event Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Content+events[1].Content+events[2].Content != "Hello "+large+"!" {
		t.Fatalf("unexpected stream: %d events", len(events))
	}
	last := events[2]
	if !last.Done || last.Tokens != 12 || last.EvalDuration != 2*time.Second {
		t.Fatalf("unexpected final event: %+v", last)
	}
}

func TestChatFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"http", 503, "not ready", "503 Service Unavailable: not ready"},
		{"service", 200, `{"error":"model not found"}`, "model not found"},
		{"malformed", 200, `not json`, "decode chat stream"},
		{"truncated", 200, "{\"message\":{\"content\":\"partial\"}}\n", "ended before completion"},
		{"empty", 200, "", "ended before completion"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			client, _ := New(server.URL)
			err := client.Chat(context.Background(), "model", nil, func(Event) {})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestChatCancellation(t *testing.T) {
	disconnected := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"message":{"content":"start"}}`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(disconnected)
	}))
	defer server.Close()
	client, _ := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := client.Chat(ctx, "model", nil, func(Event) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want cancellation", err)
	}
	select {
	case <-disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("server request was not cancelled")
	}
}

func TestInvalidEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", "localhost:8000", "ftp://deck", "http://deck?token=secret", "http://user:pass@deck", "http://deck#fragment"} {
		if _, err := New(endpoint); err == nil {
			t.Errorf("accepted invalid endpoint %q", endpoint)
		}

	}
}

func TestHailoModelManagement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/prefix/hailo/v1/list" {
			if r.Method != http.MethodGet {
				t.Errorf("catalog method = %s", r.Method)
			}
			fmt.Fprint(w, `{"models":["llama3.2:1b","qwen3:1.7b"]}`)
			return
		}
		var body struct {
			Model  string `json:"model"`
			Stream *bool  `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "llama3.2:1b" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected model payload: %+v", body)
		}
		switch r.URL.Path {
		case "/prefix/api/pull":
			if r.Method != http.MethodPost || body.Stream == nil || !*body.Stream {
				t.Errorf("unexpected pull request: %s %+v", r.Method, body)
			}
			fmt.Fprintln(w, `{"status":"downloading","total":100,"completed":50}`)
			w.(http.Flusher).Flush()
			fmt.Fprintln(w, `{"status":"success"}`)
		case "/prefix/api/delete":
			if r.Method != http.MethodDelete || body.Stream != nil {
				t.Errorf("unexpected delete request: %s %+v", r.Method, body)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL + "/prefix")
	catalog, err := client.Catalog(context.Background())
	if err != nil || len(catalog) != 2 || catalog[1] != "qwen3:1.7b" {
		t.Fatalf("catalog = %v, err = %v", catalog, err)
	}
	var progress []PullProgress
	err = client.Pull(context.Background(), "llama3.2:1b", func(p PullProgress) { progress = append(progress, p) })
	if err != nil || len(progress) != 2 || progress[0].Completed != 50 || progress[1].Status != "success" {
		t.Fatalf("progress = %v, err = %v", progress, err)
	}
	if err := client.Remove(context.Background(), "llama3.2:1b"); err != nil {
		t.Fatal(err)
	}
}

func TestPullFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"missing", 404, `{"error":"unknown model"}`, "404"},
		{"stream error", 200, "{\"status\":\"downloading\"}\n{\"error\":\"disk full\"}\n", "disk full"},
		{"malformed", 200, "not json", "decode pull stream"},
		{"truncated", 200, `{"status":"downloading","completed":100,"total":100}`, "ended before success"},
		{"empty", 200, "", "ended before success"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			client, _ := New(server.URL)
			err := client.Pull(context.Background(), "model", func(PullProgress) {})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPullCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"status":"downloading"}`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	client, _ := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := client.Pull(ctx, "model", func(PullProgress) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want cancellation", err)
	}
}

func TestRemoveFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"code":"not_found","error":"model not found"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL)
	if err := client.Remove(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("err = %v", err)
	}
}
