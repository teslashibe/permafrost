package openai

import "github.com/teslashibe/permafrost/internal/inference"

// NewProvider adapts Client to the inference.ProviderFactory signature so it
// can be passed to inference.NewRegistry without creating an import cycle.
func NewProvider(cfg inference.ProviderConfig) (inference.Provider, error) {
	return New(Config{
		Name:    cfg.Name,
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Timeout: cfg.Timeout,
	})
}
