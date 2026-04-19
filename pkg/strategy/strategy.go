// Package strategy defines the Strategy contract: the pure-logic interface
// that proposes orders and swaps in response to market state.
//
// A Strategy is stateless across restarts; all persistent state lives in the
// store. Implementations live in subpackages such as
// internal/strategy/funding_arb_basic.
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
type WarmupInput struct {
	AgentID string
	Config  map[string]any
	Now     time.Time
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
