package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/teslashibe/permafrost/internal/inference"
)

// Client implements inference.Provider against any OpenAI-compatible HTTP API.
type Client struct {
	name    string
	baseURL string
	apiKey  string
	http    *http.Client
}

// Config configures a Client.
type Config struct {
	Name    string        // identifier returned by Name(); e.g. "openrouter"
	BaseURL string        // e.g. "https://api.openai.com/v1"
	APIKey  string        // bearer token (may be empty for local providers)
	Timeout time.Duration // request timeout (default 60s)
}

// New constructs a Client. BaseURL must be set. If APIKey is empty, no
// Authorization header is sent.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("openai: BaseURL is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &Client{
		name:    cfg.Name,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		http:    &http.Client{Timeout: cfg.Timeout},
	}, nil
}

// Compile-time check.
var _ inference.Provider = (*Client)(nil)

func (c *Client) Name() string { return c.name }

// Complete sends a chat-completions request and returns the parsed response.
func (c *Client) Complete(ctx context.Context, req inference.Request) (inference.Response, error) {
	body, err := buildChatRequest(req)
	if err != nil {
		return inference.Response{}, err
	}

	start := time.Now()
	var raw chatResponse
	rawBytes, err := c.post(ctx, "/chat/completions", body, &raw)
	if err != nil {
		return inference.Response{}, err
	}
	if raw.Error != nil {
		return inference.Response{}, fmt.Errorf("%s: %s", c.name, raw.Error.Message)
	}
	if len(raw.Choices) == 0 {
		return inference.Response{}, errors.New("openai: empty choices")
	}

	choice := raw.Choices[0]
	resp := inference.Response{
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
		TokensIn:     raw.Usage.PromptTokens,
		TokensOut:    raw.Usage.CompletionTokens,
		Model:        raw.Model,
		Provider:     c.name,
		LatencyMS:    time.Since(start).Milliseconds(),
		CostUSD:      estimateCost(raw.Model, raw.Usage.PromptTokens, raw.Usage.CompletionTokens),
		Raw:          rawBytes,
	}
	if len(choice.Message.ToolCalls) > 0 {
		resp.ToolCalls = make([]inference.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			resp.ToolCalls[i] = inference.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			}
		}
	}
	return resp, nil
}

// Embed sends an embeddings request and returns the parsed response.
func (c *Client) Embed(ctx context.Context, req inference.EmbedRequest) (inference.EmbedResponse, error) {
	body := embedRequest{Model: req.Model, Input: req.Input}
	var raw embedResponse
	if _, err := c.post(ctx, "/embeddings", body, &raw); err != nil {
		return inference.EmbedResponse{}, err
	}
	if raw.Error != nil {
		return inference.EmbedResponse{}, fmt.Errorf("%s: %s", c.name, raw.Error.Message)
	}
	out := inference.EmbedResponse{
		Vectors:  make([][]float32, len(raw.Data)),
		TokensIn: raw.Usage.PromptTokens,
		Model:    raw.Model,
		Provider: c.name,
		CostUSD:  estimateCost(raw.Model, raw.Usage.PromptTokens, 0),
	}
	for i, d := range raw.Data {
		out.Vectors[i] = d.Embedding
	}
	return out, nil
}

// post issues a POST with a JSON body and decodes into out. The raw response
// bytes are also returned so callers can persist them for audit.
func (c *Client) post(ctx context.Context, path string, body any, out any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("openai: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: do: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusTooManyRequests {
		return raw, fmt.Errorf("%w (status=%d body=%s)", inference.ErrRateLimited, resp.StatusCode, string(truncate(raw)))
	}
	if resp.StatusCode/100 != 2 {
		return raw, fmt.Errorf("openai: %s -> %d: %s", path, resp.StatusCode, string(truncate(raw)))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return raw, fmt.Errorf("openai: decode: %w", err)
		}
	}
	return raw, nil
}

func truncate(b []byte) []byte {
	if len(b) > 1024 {
		return append(b[:1024:1024], []byte("...")...)
	}
	return b
}

// buildChatRequest converts an inference.Request into the wire shape.
func buildChatRequest(in inference.Request) (chatRequest, error) {
	out := chatRequest{
		Model: in.Model,
		Stop:  in.Stop,
		User:  in.User,
	}
	if in.Temperature != 0 {
		t := in.Temperature
		out.Temperature = &t
	}
	if in.TopP != 0 {
		p := in.TopP
		out.TopP = &p
	}
	if in.MaxTokens != 0 {
		mt := in.MaxTokens
		out.MaxTokens = &mt
	}

	if in.System != "" {
		out.Messages = append(out.Messages, chatMessage{Role: "system", Content: in.System})
	}
	for _, m := range in.Messages {
		out.Messages = append(out.Messages, convertMessage(m))
	}
	if len(out.Messages) == 0 {
		return chatRequest{}, errors.New("openai: at least one message (or System) is required")
	}

	for _, t := range in.Tools {
		if !json.Valid(t.Schema) {
			return chatRequest{}, fmt.Errorf("openai: tool %q: invalid JSON schema", t.Name)
		}
		out.Tools = append(out.Tools, chatTool{
			Type: "function",
			Function: chatToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(t.Schema),
			},
		})
	}

	if in.JSONSchema != nil {
		if !json.Valid(in.JSONSchema.JSON) {
			return chatRequest{}, errors.New("openai: JSONSchema.JSON is not valid JSON")
		}
		out.ResponseFormat = &responseFmt{
			Type: "json_schema",
			JSONSchema: &responseFmtSchema{
				Name:        in.JSONSchema.Name,
				Description: in.JSONSchema.Description,
				Schema:      json.RawMessage(in.JSONSchema.JSON),
				Strict:      in.JSONSchema.Strict,
			},
		}
	}
	return out, nil
}

func convertMessage(m inference.Message) chatMessage {
	out := chatMessage{
		Role:       string(m.Role),
		Content:    m.Content,
		Name:       m.Name,
		ToolCallID: m.ToolCallID,
	}
	for _, tc := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, chatToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: tc.Name, Arguments: tc.Arguments},
		})
	}
	return out
}
