// Package agent contains the runtime that drives a Strategy: scheduling,
// paired execution (spot-first), persistence, and lifecycle management.
package agent

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Mode controls whether decisions actually hit the venues.
type Mode string

const (
	ModePaper Mode = "paper" // record decisions; no venue calls
	ModeLive  Mode = "live"  // submit to venues
)

// Network selects which Hyperliquid environment the agent reads/writes
// against. Per-agent so a single deployment can run e.g. a paper-mainnet
// research agent next to a live-testnet agent under development. v1 only
// supports mainnet | testnet.
type Network string

const (
	NetworkMainnet Network = "mainnet"
	NetworkTestnet Network = "testnet"
)

// Validate returns an error if n is not one of the recognised networks.
func (n Network) Validate() error {
	switch n {
	case NetworkMainnet, NetworkTestnet, "":
		return nil
	}
	return fmt.Errorf("agent: invalid network %q (want mainnet or testnet)", n)
}

// OrDefault returns n if non-empty, else the supplied fallback. Used by
// the Loader and CLI when an agent record predates the network column.
func (n Network) OrDefault(fallback Network) Network {
	if n == "" {
		return fallback
	}
	return n
}

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
	Network        Network // hyperliquid environment (mainnet|testnet)
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
