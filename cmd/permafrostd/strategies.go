package main

// This file enables strategies in the permafrostd binary by blank-importing
// their packages so each one's init() runs and calls strategy.Register.
//
// Add a community-shipped or reference strategy here when you contribute
// it to the public repo (each new strategy gets one line).
//
// Private strategies you do not want in the public repo go in the sibling
// strategies_local.go file, which is gitignored. See STRATEGY_AUTHORS.md
// for the full extension flow.

import (
	_ "github.com/teslashibe/permafrost/strategies/noop"
)
