// Package inference defines the Provider contract: a single OpenAI-compatible
// chat-completion interface used by Permafrost strategies and runtime
// components. One implementation in internal/inference/openai serves OpenAI,
// OpenRouter, Groq, vLLM, Ollama, etc. via base_url.
//
// Strategies depend on inference.Provider, never on a concrete client. Native
// non-OpenAI SDKs (Anthropic Messages, Gemini, Bedrock) may be added later
// as additional implementations of Provider without touching strategies.
package inference

import (
	"context"
	"errors"
)

// Role labels the speaker of a Message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one turn in a conversation.
type Message struct {
	Role       Role        `json:"role"`
	Content    string      `json:"content"`
	Name       string      `json:"name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// ToolSpec describes a function the model may call. The Schema is a JSON
// Schema document for the function's arguments.
type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Schema      []byte `json:"schema"` // raw JSON Schema
}

// ToolCall is a model-issued invocation of a registered tool.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // raw JSON
}

// Schema instructs the model to return JSON conforming to the supplied schema.
type Schema struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	JSON        []byte `json:"json"` // raw JSON Schema
	Strict      bool   `json:"strict"`
}

// Request is a chat-completion request.
type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolSpec
	JSONSchema  *Schema
	Temperature float64
	TopP        float64
	MaxTokens   int
	Stop        []string
	User        string // pass-through for providers that support it
}

// Response is what Provider.Complete returns.
type Response struct {
	Content      string     // primary message body
	ToolCalls    []ToolCall // populated when the model issued tool calls
	FinishReason string     // "stop" | "length" | "tool_calls" | provider-specific
	TokensIn     int
	TokensOut    int
	Model        string
	Provider     string
	LatencyMS    int64
	CostUSD      float64 // 0 if provider/model pricing unknown
	Raw          []byte  // opaque provider response for audit/debug
}

// EmbedRequest asks the provider to embed one or more inputs.
type EmbedRequest struct {
	Model string
	Input []string
}

// EmbedResponse carries the resulting vectors.
type EmbedResponse struct {
	Vectors  [][]float32
	TokensIn int
	Model    string
	Provider string
	CostUSD  float64
}

// Provider is the single inference contract. Implementations MUST be safe
// for concurrent use.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (Response, error)
	Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error)
}

// ErrUnsupportedFeature is returned when the underlying provider/model does
// not support the requested feature (e.g. tool calls on a base Ollama model).
// Strategies SHOULD handle this with a graceful fallback (e.g. ask for JSON
// in the prompt and parse it).
var ErrUnsupportedFeature = errors.New("inference: unsupported feature")

// ErrRateLimited is returned when the provider signals a rate-limit / 429.
var ErrRateLimited = errors.New("inference: rate limited")
