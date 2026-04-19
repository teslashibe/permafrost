// Package risk defines the Engine contract: pre-trade and portfolio-level
// risk checks. Concrete engines (built in M9) live in this package.
package risk

import (
	"context"

	"github.com/teslashibe/permafrost/pkg/types"
)

// Engine evaluates trade intents and portfolio state against agent limits
// and global circuit breakers.
//
// PreTrade is called once per intent (OrderIntent or SwapIntent) emitted by
// a Strategy. The agent runtime drops any intent that returns a Block verdict
// and emits a metric+log on Warn verdicts.
//
// Portfolio is called periodically (and after every fill/swap) against the
// consolidated PortfolioSnapshot; a Block verdict triggers an automatic
// agent halt and (configurably) a kill-switch unwind.
type Engine interface {
	PreTrade(ctx context.Context, agentID string, intent any, snap types.PortfolioSnapshot) types.Verdict
	Portfolio(ctx context.Context, snap types.PortfolioSnapshot) types.Verdict
}
