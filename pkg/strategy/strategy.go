// Package strategy defines the Permafrost Strategy contract: the pure-logic
// interface that proposes orders and swaps in response to market state.
//
// This package is the stable public SAPI. New strategies live as sibling
// subdirectories under strategies/ in the repo root (e.g. strategies/noop,
// strategies/<your_name>) and depend only on this package and pkg/types.
//
// Lifecycle:
//   - Register a Constructor under a stable snake_case name in init().
//   - The agent runtime calls Warmup(ctx, WarmupInput) once after
//     construction, supplying framework Services (logger, inference, ...).
//   - The runtime then calls Decide(ctx, DecisionInput) on every tick.
//     Implementations MUST be deterministic given DecisionInput.
//
// A Strategy is stateless across process restarts; all persistent state
// lives in the framework's store and is rehydrated into DecisionInput.
package strategy

import (
	"context"
	"time"

	"github.com/teslashibe/permafrost/pkg/types"
)

// Strategy is the framework's primary contract. The agent runtime calls
// Warmup once after creation and Decide on every scheduled tick.
type Strategy interface {
	// Name is a stable identifier used in configs, the registry, and the DB.
	Name() string

	// Warmup is called once after the strategy is constructed. Implementations
	// may use it to fetch initial state, validate config, or pre-compute
	// indicators. It MUST be idempotent.
	Warmup(ctx context.Context, in WarmupInput) error

	// Decide produces the next batch of intents given the current state.
	// Implementations MUST be deterministic given DecisionInput; any random
	// or external dependency should be threaded through Signals or be opaque
	// to the framework.
	Decide(ctx context.Context, in DecisionInput) (Decision, error)
}

// WarmupInput is supplied once at strategy construction time.
//
// Universe is the per-agent symbol list from agents.universe; strategies
// that filter by symbol should respect it.
//
// Services carries framework-provided dependencies (logger, inference
// provider, etc.). Strategies that require a service MUST check it is
// non-nil and return an error from Warmup if it is missing — the
// framework will refuse to start the agent.
type WarmupInput struct {
	AgentID  string
	Universe []string
	Config   map[string]any
	Now      time.Time
	Services Services
}

// DecisionInput carries everything the strategy needs to make a single
// decision tick. The framework guarantees:
//   - Cash, Positions, BasisPositions reflect on-chain/venue state at Now.
//   - Market is consistent across symbols (same wall-clock fetch round).
//   - Signals is opaque to the framework; strategies may populate it with
//     LLM outputs or computed indicators between ticks.
type DecisionInput struct {
	AgentID         string
	Now             time.Time
	Market          types.MarketSnapshot
	PerpPositions   []types.Position
	SpotBalances    []types.WalletBalance
	BasisPositions  []types.BasisPosition
	Cash            map[string]types.Balance // keyed by location: "hyperliquid", "solana"
	Signals         map[string]any
	Limits          types.RiskLimits
}

// Decision is a Strategy's response to one DecisionInput. The agent runtime
// executes Swaps before Orders to preserve the spot-first invariant for
// basis strategies.
//
// A strategy that owns the paired-execution invariant should:
//   - on entry: emit a SwapIntent (long spot) and an OrderIntent (short perp)
//     in the same Decision; Swaps run and confirm before Orders;
//   - on partial state recovery: omit one of the legs and rely on the next
//     tick to reconcile.
type Decision struct {
	Orders     []types.OrderIntent
	Swaps      []types.SwapIntent
	Cancels    []types.OrderID
	Notes      string  // human/LLM-readable rationale
	Confidence float64 // 0..1; advisory
}

// HasIntents reports whether the decision proposes any state-changing action.
func (d Decision) HasIntents() bool {
	return len(d.Orders) > 0 || len(d.Swaps) > 0 || len(d.Cancels) > 0
}
