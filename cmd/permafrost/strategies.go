package main

// This file enables strategies in the permafrost CLI binary by
// blank-importing their packages so each one's init() runs and calls
// strategy.Register. The CLI uses the registry for `strategy list` and
// `strategy backtest <name>`, so a strategy needs to be linked here
// (or via strategies_local.go) to be backtest-able.
//
// This is the symmetric mate of cmd/permafrostd/strategies.go: when
// you contribute a strategy upstream, add it to BOTH files (one line
// each). When you keep a strategy private, add it to BOTH
// strategies_local.go files (each gitignored).
//
// See STRATEGY_AUTHORS.md for the full extension flow.

import (
	_ "github.com/teslashibe/permafrost/strategies/noop"
)
