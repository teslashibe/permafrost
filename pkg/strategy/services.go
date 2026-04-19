package strategy

import (
	"log/slog"

	"github.com/teslashibe/permafrost/pkg/inference"
)

// Services is the bag of framework-provided dependencies that strategies
// receive in WarmupInput. Fields may be nil (or zero-valued) if the
// operator has not configured the corresponding feature; strategies that
// require a particular service MUST validate its presence in Warmup and
// return an error if it is missing.
//
// Services is the canonical extension point for new framework features
// that strategies may depend on. Add fields here rather than expanding
// WarmupInput or DecisionInput directly.
type Services struct {
	// Logger is an agent-scoped slog logger. The framework guarantees
	// it is non-nil; if no logger was wired by the runtime, a default
	// slog.Default() is supplied.
	Logger *slog.Logger

	// Inference is the LLM provider for this agent, resolved from the
	// agent's configured "provider:model" string. nil if the operator
	// has not configured an inference provider for this agent.
	Inference inference.Provider
}
