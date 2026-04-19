// Package funding_arb_basic implements the first Permafrost trading strategy:
// a delta-neutral funding-rate arb that goes long spot on Solana via Jupiter
// and short perp on Hyperliquid. The strategy is deterministic; the LLM is
// optionally consulted to VETO new entries based on event/news context, but
// it never invents trades.
//
// See SCOPE.md §14 for the design rationale.
package funding_arb_basic

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// Name is the registered identifier for this strategy.
const Name = "funding_arb_basic"

// Config controls strategy thresholds. All fields have safe defaults so an
// agent can be created with an empty config and still run a sensible baseline.
type Config struct {
	// Annualised funding (fractional, e.g. 0.50 = 50% APR) at which a new
	// basis position is opened. Default 0.50.
	EntryAnnualisedFunding decimal.Decimal

	// Annualised funding below which an open basis is closed.
	// Default 0.10.
	ExitAnnualisedFunding decimal.Decimal

	// Max notional per basis position in USDC.
	PositionCapUSDC decimal.Decimal

	// Don't reopen a symbol within this window of its last close.
	// Default 4h.
	PerSymbolCooldown time.Duration

	// Max basis (perp - spot) at entry, in basis points. 0 disables.
	MaxEntryBasisBps int

	// Slippage budget for spot swaps (bps).
	SlippageBps int

	// If true, ask the inference provider for a veto on each candidate.
	UseLLMVeto bool

	// Model to use for the veto. e.g. "openrouter/anthropic/claude-sonnet-4.5"
	// or "gpt-5-mini".
	VetoModel string

	// Universe override; empty defaults to the tradable subset of the
	// registry intersected with the agent's universe.
	Universe []string
}

// Defaults applies sensible defaults to zero-valued fields.
func (c *Config) Defaults() {
	if c.EntryAnnualisedFunding.IsZero() {
		c.EntryAnnualisedFunding = decimal.NewFromFloat(0.50)
	}
	if c.ExitAnnualisedFunding.IsZero() {
		c.ExitAnnualisedFunding = decimal.NewFromFloat(0.10)
	}
	if c.PositionCapUSDC.IsZero() {
		c.PositionCapUSDC = decimal.NewFromInt(1000)
	}
	if c.PerSymbolCooldown == 0 {
		c.PerSymbolCooldown = 4 * time.Hour
	}
	if c.SlippageBps == 0 {
		c.SlippageBps = 50
	}
}

// Validate checks the config for sanity.
func (c Config) Validate() error {
	if c.EntryAnnualisedFunding.LessThanOrEqual(c.ExitAnnualisedFunding) {
		return errors.New("funding_arb_basic: entry threshold must be greater than exit threshold")
	}
	if !c.PositionCapUSDC.IsPositive() {
		return errors.New("funding_arb_basic: PositionCapUSDC must be positive")
	}
	if c.SlippageBps < 0 || c.SlippageBps > 1000 {
		return errors.New("funding_arb_basic: SlippageBps out of range (0..1000)")
	}
	return nil
}
