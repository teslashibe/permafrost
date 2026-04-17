// Package openai implements an OpenAI-compatible chat-completions client.
//
// The same client serves OpenAI, OpenRouter, Groq, Together, Fireworks, vLLM,
// Ollama, LM Studio, and any other provider that exposes the OpenAI Chat
// Completions schema at /chat/completions and embeddings at /embeddings.
//
// Provider selection is done via base_url; this package contains zero
// per-provider branching.
package openai

import "encoding/json"

// chatRequest is the JSON body sent to /chat/completions.
type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	Temperature    *float64       `json:"temperature,omitempty"`
	TopP           *float64       `json:"top_p,omitempty"`
	MaxTokens      *int           `json:"max_tokens,omitempty"`
	Stop           []string       `json:"stop,omitempty"`
	User           string         `json:"user,omitempty"`
	Tools          []chatTool     `json:"tools,omitempty"`
	ResponseFormat *responseFmt   `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"` // always "function"
	Function chatToolFunc `json:"function"`
}

type chatToolFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type responseFmt struct {
	Type       string             `json:"type"` // "json_schema" | "json_object"
	JSONSchema *responseFmtSchema `json:"json_schema,omitempty"`
}

type responseFmtSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      bool            `json:"strict,omitempty"`
}

// chatResponse is the JSON body returned by /chat/completions.
type chatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Created int64  `json:"created"`
	Choices []struct {
		Index        int         `json:"index"`
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *apiError `json:"error,omitempty"`
}

type apiError struct {
	Message string      `json:"message"`
	Type    string      `json:"type"`
	Code    interface{} `json:"code"` // string or int depending on provider
}

// embedRequest is the JSON body sent to /embeddings.
type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embedResponse is the JSON body returned by /embeddings.
type embedResponse struct {
	Model string `json:"model"`
	Data  []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
	Error *apiError `json:"error,omitempty"`
}
