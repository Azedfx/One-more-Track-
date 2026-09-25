package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Client speaks the handbook MCP at BITGET_US_MCP_URL.
// The server exposes two tools: guide and do_query.
type Client struct {
	url  string
	http *http.Client
	mu   sync.Mutex
	sess string
}

func New(url string) *Client {
	return &Client{
		url: url,
		http: &http.Client{
			Timeout: 40 * time.Second,
		},
	}
}

// Query runs one catalog entry and returns the decoded do_query payload.
func (c *Client) Query(ctx context.Context, entryID string, params map[string]any) (QueryResult, error) {
	raw, err := c.call(ctx, "do_query", map[string]any{
		"entry_id": entryID,
		"params":   params,
	})
	if err != nil {
		return QueryResult{}, err
	}
	var out QueryResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return QueryResult{}, fmt.Errorf("do_query %s: %w", entryID, err)
	}
	return out, nil
}

type QueryResult struct {
	Success    bool            `json:"success"`
	StatusCode int             `json:"status_code"`
	Data       json.RawMessage `json:"data"`
	Error      json.RawMessage `json:"error"`
}

func (c *Client) call(ctx context.Context, name string, args any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensure(ctx); err != nil {
		return nil, err
	}
	text, err := c.tool(ctx, name, args)
	if err != nil {
		// One fresh session, then fail.
		c.sess = ""
		if err2 := c.ensure(ctx); err2 != nil {
			return nil, err
		}
		text, err = c.tool(ctx, name, args)
		if err != nil {
			return nil, err
		}
	}
	return text, nil
}

func (c *Client) ensure(ctx context.Context) error {
	if c.sess != "" {
		return nil
	}
	body := rpcBody("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "regime-sleeve", "version": "0.1.0"},
	})
	resp, err := c.post(ctx, body, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("mcp initialize HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(b))
	}
	sid := resp.Header.Get("mcp-session-id")
	if sid == "" {
		return fmt.Errorf("mcp initialize returned no session id")
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	c.sess = sid
	note, err := c.post(ctx, rpcBody("notifications/initialized", map[string]any{}), sid)
	if err != nil {
		c.sess = ""
		return err
	}
	note.Body.Close()
	return nil
}

func (c *Client) tool(ctx context.Context, name string, args any) (json.RawMessage, error) {
	resp, err := c.post(ctx, rpcBody("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	}), c.sess)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp %s HTTP %d", name, resp.StatusCode)
	}
	msg, err := firstJSON(buf)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil {
		return nil, fmt.Errorf("mcp envelope: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("mcp %s: %s", name, envelope.Error.Message)
	}
	if envelope.Result.IsError {
		return nil, fmt.Errorf("mcp %s tool error", name)
	}
	if len(envelope.Result.Content) == 0 || envelope.Result.Content[0].Text == "" {
		return nil, fmt.Errorf("mcp %s: empty content", name)
	}
	return json.RawMessage(envelope.Result.Content[0].Text), nil
}

func (c *Client) post(ctx context.Context, body []byte, session string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("mcp-session-id", session)
	}
	return c.http.Do(req)
}

func rpcBody(method string, params any) []byte {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if method == "notifications/initialized" {
		delete(payload, "id")
	}
	if params != nil {
		payload["params"] = params
	}
	b, _ := json.Marshal(payload)
	return b
}

func firstJSON(body []byte) (json.RawMessage, error) {
	if json.Valid(bytes.TrimSpace(body)) {
		return bytes.TrimSpace(body), nil
	}
	var last []byte
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		last = payload
	}
	if json.Valid(last) {
		return last, nil
	}
	return nil, fmt.Errorf("mcp response had no json payload")
}
