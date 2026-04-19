package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/pkg/inference"
)

func newServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{Name: "test", BaseURL: srv.URL, APIKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	return srv, c
}

func TestComplete_HappyPath(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("auth: %s", got)
		}
		body, _ := io.ReadAll(r.Body)
		var got chatRequest
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode: %v body=%s", err, body)
		}
		if got.Model != "gpt-4o" {
			t.Errorf("model: %s", got.Model)
		}
		if len(got.Messages) != 2 {
			t.Errorf("messages: %d", len(got.Messages))
		}
		if got.Messages[0].Role != "system" || got.Messages[1].Role != "user" {
			t.Errorf("roles: %+v", got.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-1",
			"model":   "gpt-4o",
			"created": 0,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "hi there",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		})
	})

	resp, err := c.Complete(context.Background(), inference.Request{
		Model:  "gpt-4o",
		System: "you are helpful",
		Messages: []inference.Message{
			{Role: inference.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hi there" {
		t.Errorf("Content: %q", resp.Content)
	}
	if resp.TokensIn != 10 || resp.TokensOut != 5 {
		t.Errorf("tokens: in=%d out=%d", resp.TokensIn, resp.TokensOut)
	}
	if resp.Provider != "test" {
		t.Errorf("Provider: %q", resp.Provider)
	}
	if resp.CostUSD == 0 {
		t.Errorf("CostUSD: gpt-4o should have a non-zero estimate, got %f", resp.CostUSD)
	}
	if len(resp.Raw) == 0 {
		t.Errorf("Raw should be populated")
	}
}

func TestComplete_ToolCalls(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "gpt-4o",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []map[string]any{{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "get_funding",
							"arguments": `{"coin":"WIF"}`,
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
		})
	})
	resp, err := c.Complete(context.Background(), inference.Request{
		Model: "gpt-4o", Messages: []inference.Message{{Role: inference.RoleUser, Content: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "get_funding" {
		t.Errorf("ToolCalls: %+v", resp.ToolCalls)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("FinishReason: %q", resp.FinishReason)
	}
}

func TestComplete_RateLimited(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	})
	_, err := c.Complete(context.Background(), inference.Request{
		Model: "gpt-4o", System: "x",
	})
	if !errors.Is(err, inference.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestComplete_APIError(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "model not found"},
		})
	})
	_, err := c.Complete(context.Background(), inference.Request{
		Model: "gpt-4o", System: "x",
	})
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("expected error with API message, got %v", err)
	}
}

func TestComplete_RequiresMessage(t *testing.T) {
	c, _ := New(Config{BaseURL: "http://localhost"})
	_, err := c.Complete(context.Background(), inference.Request{Model: "x"})
	if err == nil {
		t.Fatal("expected error for empty messages")
	}
}

func TestEmbed_HappyPath(t *testing.T) {
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "text-embedding-3-small",
			"data": []map[string]any{
				{"embedding": []float32{0.1, 0.2}, "index": 0},
				{"embedding": []float32{0.3, 0.4}, "index": 1},
			},
			"usage": map[string]any{"prompt_tokens": 4, "total_tokens": 4},
		})
	})
	resp, err := c.Embed(context.Background(), inference.EmbedRequest{
		Model: "text-embedding-3-small",
		Input: []string{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Vectors) != 2 || resp.Vectors[0][0] != 0.1 {
		t.Errorf("vectors: %+v", resp.Vectors)
	}
	if resp.TokensIn != 4 {
		t.Errorf("TokensIn: %d", resp.TokensIn)
	}
}

func TestPricing_KnownAndUnknown(t *testing.T) {
	if got := estimateCost("gpt-4o", 1_000_000, 1_000_000); got <= 0 {
		t.Errorf("known model should cost > 0, got %f", got)
	}
	if got := estimateCost("anthropic/claude-sonnet-4.5", 1_000_000, 0); got != 3.0 {
		t.Errorf("suffix match should cost 3.0, got %f", got)
	}
	if got := estimateCost("totally-made-up-model", 100, 100); got != 0 {
		t.Errorf("unknown model should cost 0, got %f", got)
	}
}
