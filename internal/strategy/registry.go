package strategy

import (
	"fmt"
	"sort"
	"sync"
)

// Constructor builds a Strategy from a config blob. Each implementation
// registers a Constructor in init() so the agent runtime can instantiate
// strategies by name (matching the agents.strategy column in the DB).
type Constructor func(cfg map[string]any) (Strategy, error)

var (
	regMu       sync.RWMutex
	constructors = map[string]Constructor{}
)

// Register adds a strategy constructor under the given name. It panics on
// double registration to surface bugs at process start. Names should be
// snake_case (e.g. "funding_arb_basic").
func Register(name string, c Constructor) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := constructors[name]; dup {
		panic(fmt.Sprintf("strategy %q already registered", name))
	}
	constructors[name] = c
}

// Get returns the constructor registered for name, or an error if no such
// strategy is registered.
func Get(name string) (Constructor, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	c, ok := constructors[name]
	if !ok {
		return nil, fmt.Errorf("strategy %q not registered", name)
	}
	return c, nil
}

// List returns all registered strategy names in sorted order.
func List() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(constructors))
	for name := range constructors {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Reset clears the registry. For tests only.
func Reset() {
	regMu.Lock()
	defer regMu.Unlock()
	constructors = map[string]Constructor{}
}
