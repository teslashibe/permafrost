// Package noop provides a permissive Engine that allows everything. For
// tests only — never wire this into a live agent.
package noop

import (
	"context"

	"github.com/teslashibe/permafrost/internal/risk"
	"github.com/teslashibe/permafrost/pkg/types"
)

// Engine allows every intent and reports the portfolio as healthy.
type Engine struct{}

// New constructs the engine.
func New() *Engine { return &Engine{} }

// Compile-time check.
var _ risk.Engine = (*Engine)(nil)

func (Engine) PreTrade(_ context.Context, _ string, _ any, _ types.PortfolioSnapshot) types.Verdict {
	return types.Verdict{Kind: types.VerdictAllow}
}

func (Engine) Portfolio(_ context.Context, _ types.PortfolioSnapshot) types.Verdict {
	return types.Verdict{Kind: types.VerdictAllow}
}

// Limits returns the zero RiskLimits — i.e. no limits enforced.
// Killswitch callers asking the noop engine for its slippage cap will
// fall back to whatever default they hold (100bps in the killswitch).
func (Engine) Limits() types.RiskLimits { return types.RiskLimits{} }
