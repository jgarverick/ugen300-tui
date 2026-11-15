package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Model struct {
	Name string `json:"name"`
}

type Event struct {
	Content      string
	Done         bool
	Tokens       int
	EvalDuration time.Duration
}

type PullProgress struct {
	Status    string `json:"status"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

func New(endpoint string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, errors.New("endpoint must be an http:// or https:// URL without credentials, query, or fragment")
	}
	return &Client{
		baseURL: strings.TrimRight(endpoint, "/"),
		http: &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 2 * time.Minute,
			IdleConnTimeout:       90 * time.Second,
		}},
	}, nil
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return resp, nil
}

func (c *Client) Models(ctx context.Context) ([]Model, error) {
	resp, err := c.request(ctx, http.MethodGet, "/api/tags", nil)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	var result struct {
		Models []Model `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}
	return result.Models, nil
}

func (c *Client) Catalog(ctx context.Context) ([]string, error) {
	resp, err := c.request(ctx, http.MethodGet, "/hailo/v1/list", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	return result.Models, nil
}

func (c *Client) Pull(ctx context.Context, model string, emit func(PullProgress)) error {
	body, err := json.Marshal(struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}{model, true})
	if err != nil {
		return err
	}
	resp, err := c.request(ctx, http.MethodPost, "/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var chunk struct {
			PullProgress
			Error string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			return fmt.Errorf("decode pull stream: %w", err)
		}
		if chunk.Error != "" {
			return errors.New(chunk.Error)
		}
		emit(chunk.PullProgress)
		if chunk.Status == "success" {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read pull stream: %w", err)
	}
	return errors.New("pull stream ended before success")
}

func (c *Client) Remove(ctx context.Context, model string) error {
	body, err := json.Marshal(struct {
		Model string `json:"model"`
	}{model})
	if err != nil {
		return err
	}
	resp, err := c.request(ctx, http.MethodDelete, "/api/delete", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Hailo returns HTTP 200 with an empty body on successful deletion.
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
	return err
}
func (c *Client) Chat(ctx context.Context, model string, messages []Message, emit func(Event)) error {
	body, err := json.Marshal(struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
		Stream   bool      `json:"stream"`
	}{model, messages, true})
	if err != nil {
		return err
	}
	resp, err := c.request(ctx, http.MethodPost, "/api/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var chunk struct {
			Message      Message `json:"message"`
			Done         bool    `json:"done"`
			Error        string  `json:"error"`
			EvalCount    int     `json:"eval_count"`
			EvalDuration int64   `json:"eval_duration"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			return fmt.Errorf("decode chat stream: %w", err)
		}
		if chunk.Error != "" {
			return errors.New(chunk.Error)
		}
		emit(Event{
			Content: chunk.Message.Content, Done: chunk.Done,
			Tokens: chunk.EvalCount, EvalDuration: time.Duration(chunk.EvalDuration),
		})
		if chunk.Done {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read chat stream: %w", err)
	}
	return errors.New("chat stream ended before completion")
}
