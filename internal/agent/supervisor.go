package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// Supervisor manages a set of Runtimes by agent ID. It is the single owner
// of process-wide kill-switch behaviour.
type Supervisor struct {
	logger *slog.Logger

	mu       sync.Mutex
	runtimes map[string]*Runtime
}

// NewSupervisor returns an empty Supervisor.
func NewSupervisor(log *slog.Logger) *Supervisor {
	if log == nil {
		log = slog.Default()
	}
	return &Supervisor{
		logger:   log,
		runtimes: make(map[string]*Runtime),
	}
}

// Register adds a Runtime to the supervisor. If an entry already exists for
// the agent ID, it is replaced (and the old one is stopped).
func (s *Supervisor) Register(agentID string, r *Runtime) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.runtimes[agentID]; ok {
		_ = old.Stop(context.Background(), "replaced")
	}
	s.runtimes[agentID] = r
}

// Get returns the Runtime registered for agentID.
func (s *Supervisor) Get(agentID string) (*Runtime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runtimes[agentID]
	if !ok {
		return nil, fmt.Errorf("supervisor: agent %q not registered", agentID)
	}
	return r, nil
}

// IDs returns all registered agent IDs.
func (s *Supervisor) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.runtimes))
	for id := range s.runtimes {
		out = append(out, id)
	}
	return out
}

// Start launches a registered Runtime.
func (s *Supervisor) Start(ctx context.Context, agentID string) error {
	r, err := s.Get(agentID)
	if err != nil {
		return err
	}
	return r.Start(ctx)
}

// Stop halts a registered Runtime gracefully.
func (s *Supervisor) Stop(ctx context.Context, agentID, reason string) error {
	r, err := s.Get(agentID)
	if err != nil {
		return err
	}
	return r.Stop(ctx, reason)
}

// Trip triggers the kill switch on a single agent.
func (s *Supervisor) Trip(ctx context.Context, agentID string, opts KillSwitchOptions) error {
	r, err := s.Get(agentID)
	if err != nil {
		return err
	}
	return r.Trip(ctx, opts)
}

// TripAll triggers the kill switch across every registered agent.
// Errors are joined; the supervisor still attempts every agent before
// returning.
func (s *Supervisor) TripAll(ctx context.Context, opts KillSwitchOptions) error {
	s.mu.Lock()
	rs := make([]*Runtime, 0, len(s.runtimes))
	for _, r := range s.runtimes {
		rs = append(rs, r)
	}
	s.mu.Unlock()

	var errs []error
	for _, r := range rs {
		if err := r.Trip(ctx, opts); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
