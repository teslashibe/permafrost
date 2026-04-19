package inference

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/teslashibe/permafrost/internal/config"
)

// Registry holds the configured Provider set, keyed by name. Construct one
// with NewRegistry. The registry is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	def      string
	provs    map[string]Provider
}

// NewRegistry builds a Registry from configuration. It instantiates one
// OpenAI-compatible Client per named provider entry. The factory parameter
// is the constructor for an OpenAI-compatible client; it is supplied so this
// package does not import internal/inference/openai (avoiding an import
// cycle when the openai package imports this).
//
// In practice callers use:
//
//	import "github.com/teslashibe/permafrost/pkg/inference/openai"
//	reg, err := inference.NewRegistry(cfg.Inference, openai.NewProvider)
func NewRegistry(cfg config.InferenceConfig, factory ProviderFactory) (*Registry, error) {
	r := &Registry{
		def:   cfg.Default,
		provs: make(map[string]Provider, len(cfg.Providers)),
	}
	if len(cfg.Providers) == 0 {
		return r, nil
	}

	for name, pcfg := range cfg.Providers {
		if pcfg.BaseURL == "" {
			return nil, fmt.Errorf("inference provider %q: base_url is required", name)
		}
		key := pcfg.APIKey
		if key == "" && pcfg.APIKeyEnv != "" {
			key = os.Getenv(pcfg.APIKeyEnv)
		}
		timeout := time.Duration(pcfg.RequestTimeoutSecs) * time.Second
		if timeout == 0 {
			timeout = 60 * time.Second
		}
		p, err := factory(ProviderConfig{
			Name:    name,
			BaseURL: pcfg.BaseURL,
			APIKey:  key,
			Timeout: timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("inference provider %q: %w", name, err)
		}
		r.provs[name] = p
	}

	if r.def == "" {
		// pick a deterministic default if none specified (alphabetical)
		names := r.Names()
		if len(names) > 0 {
			r.def = names[0]
		}
	}
	if _, ok := r.provs[r.def]; r.def != "" && !ok {
		return nil, fmt.Errorf("inference default %q not in providers", r.def)
	}
	return r, nil
}

// ProviderConfig is the input to a ProviderFactory.
type ProviderConfig struct {
	Name    string
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

// ProviderFactory constructs a Provider. It is supplied by client packages
// (e.g. internal/inference/openai) to avoid cycles.
type ProviderFactory func(ProviderConfig) (Provider, error)

// Default returns the default Provider, or an error if none is registered.
func (r *Registry) Default() (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.def == "" {
		return nil, fmt.Errorf("inference: no default provider configured")
	}
	p, ok := r.provs[r.def]
	if !ok {
		return nil, fmt.Errorf("inference: default %q missing", r.def)
	}
	return p, nil
}

// Get returns the Provider registered under name.
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.provs[name]
	if !ok {
		return nil, fmt.Errorf("inference: provider %q not registered", name)
	}
	return p, nil
}

// Names returns all registered provider names in sorted order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.provs))
	for n := range r.provs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// DefaultName returns the configured default provider name (or "").
func (r *Registry) DefaultName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.def
}
