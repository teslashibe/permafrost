// Package agent contains the runtime that drives a Strategy: scheduling,
// paired execution (spot-first), persistence, and lifecycle management.
package agent

import (
	"time"

	"github.com/shopspring/decimal"
)

// Mode controls whether decisions actually hit the venues.
type Mode string

const (
	ModePaper Mode = "paper" // record decisions; no venue calls
	ModeLive  Mode = "live"  // submit to venues
)

// Status is the persisted lifecycle state.
type Status string

const (
	StatusStopped Status = "stopped"
	StatusRunning Status = "running"
	StatusHalted  Status = "halted" // tripped a circuit breaker
)

// Agent is the persisted definition.
type Agent struct {
	ID             string
	Name           string
	Strategy       string
	Mode           Mode
	PerpVenue      string
	SpotVenue      string
	Inference      string // "provider:model"
	Universe       []string
	AllocationUSDC decimal.Decimal
	TickSecs       int
	Status         Status
	Config         map[string]any
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// AgentRun is one invocation of the runtime for an Agent.
type AgentRun struct {
	ID         int64
	AgentID    string
	StartedAt  time.Time
	EndedAt    *time.Time
	ExitReason string
}

// Tick interval default.
func (a Agent) Interval() time.Duration {
	if a.TickSecs <= 0 {
		return 60 * time.Second
	}
	return time.Duration(a.TickSecs) * time.Second
}
