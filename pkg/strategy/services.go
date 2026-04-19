package strategy

import (
	"log/slog"

	"github.com/teslashibe/permafrost/internal/assets"
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
//
// Note on package boundary: this struct intentionally references the
// concrete internal/assets.Registry rather than a redeclared interface.
// Permafrost is a single-module repo (Hummingbot model) and strategies
// already live in the same module as assets, so the import is allowed.
// If we ever want strategies to live in a separate module, define an
// AssetRegistry interface here and have internal/assets satisfy it.
type Services struct {
	// Logger is an agent-scoped slog logger. The framework guarantees
	// it is non-nil; if no logger was wired by the runtime, a default
	// slog.Default() is supplied.
	Logger *slog.Logger

	// Inference is the LLM provider for this agent, resolved by the
	// framework from the provider name in agent.Inference. nil when
	// the operator has not configured an inference provider for this
	// agent (in which case agent.Inference was empty or unparseable).
	Inference inference.Provider

	// InferenceModel is the model identifier the operator chose for
	// this agent (the part after ":" in agent.Inference). May be
	// empty. Strategies may use this as a default model and override
	// via their own typed config.
	InferenceModel string

	// Registry is the curated asset registry (perp ↔ spot mapping).
	// Always non-nil when the framework constructs Services; the
	// embedded copy is loaded on every BuildDeps call so it is
	// immediately available in Warmup and Decide.
	Registry assets.Registry
}
