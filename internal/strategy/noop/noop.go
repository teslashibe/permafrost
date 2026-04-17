// Package noop provides a Strategy that emits no intents. Useful for tests
// of the agent runtime and as a reference implementation of the interface.
package noop

import (
	"context"

	"github.com/teslashibe/permafrost/internal/strategy"
)

// Name is the registered identifier for this strategy.
const Name = "noop"

// Strategy returns no intents. Decide always succeeds.
type Strategy struct{}

// New constructs a no-op strategy. The cfg argument is ignored.
func New(_ map[string]any) (strategy.Strategy, error) { return &Strategy{}, nil }

func (Strategy) Name() string                                                       { return Name }
func (Strategy) Warmup(context.Context, strategy.WarmupInput) error                 { return nil }
func (Strategy) Decide(context.Context, strategy.DecisionInput) (strategy.Decision, error) {
	return strategy.Decision{Notes: "noop"}, nil
}

func init() { strategy.Register(Name, New) }
