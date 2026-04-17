package inference_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teslashibe/permafrost/internal/config"
	"github.com/teslashibe/permafrost/internal/inference"
)

// stubProvider records calls and lets tests assert on construction args.
type stubProvider struct {
	cfg inference.ProviderConfig
}

func (p *stubProvider) Name() string { return p.cfg.Name }
func (p *stubProvider) Complete(_ context.Context, _ inference.Request) (inference.Response, error) {
	return inference.Response{Provider: p.cfg.Name}, nil
}
func (p *stubProvider) Embed(_ context.Context, _ inference.EmbedRequest) (inference.EmbedResponse, error) {
	return inference.EmbedResponse{Provider: p.cfg.Name}, nil
}

func stubFactory(cfg inference.ProviderConfig) (inference.Provider, error) {
	return &stubProvider{cfg: cfg}, nil
}

func TestRegistry_Build(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-from-env")
	cfg := config.InferenceConfig{
		Default: "openai",
		Providers: map[string]config.InferenceProviderConfig{
			"openai":  {BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY"},
			"openrouter": {BaseURL: "https://openrouter.ai/api/v1", APIKey: "sk-direct"},
			"ollama":  {BaseURL: "http://localhost:11434/v1"},
		},
	}
	r, err := inference.NewRegistry(cfg, stubFactory)
	if err != nil {
		t.Fatal(err)
	}
	names := r.Names()
	if len(names) != 3 {
		t.Errorf("Names: got %v", names)
	}

	def, err := r.Default()
	if err != nil {
		t.Fatal(err)
	}
	if def.Name() != "openai" {
		t.Errorf("Default: %q", def.Name())
	}
	defStub, ok := def.(*stubProvider)
	if !ok || defStub.cfg.APIKey != "sk-from-env" {
		t.Errorf("openai apikey from env: got %+v", defStub.cfg)
	}

	or, _ := r.Get("openrouter")
	if or.(*stubProvider).cfg.APIKey != "sk-direct" {
		t.Errorf("openrouter apikey direct: got %+v", or.(*stubProvider).cfg)
	}

	ollama, _ := r.Get("ollama")
	if ollama.(*stubProvider).cfg.APIKey != "" {
		t.Errorf("ollama apikey should be empty: got %+v", ollama.(*stubProvider).cfg)
	}
	if ollama.(*stubProvider).cfg.Timeout == 0 {
		t.Errorf("Timeout default should be applied")
	}
}

func TestRegistry_DefaultMissing(t *testing.T) {
	cfg := config.InferenceConfig{
		Default: "nope",
		Providers: map[string]config.InferenceProviderConfig{
			"openai": {BaseURL: "x"},
		},
	}
	_, err := inference.NewRegistry(cfg, stubFactory)
	if err == nil {
		t.Fatal("expected error for missing default")
	}
}

func TestRegistry_NoProviders(t *testing.T) {
	r, err := inference.NewRegistry(config.InferenceConfig{}, stubFactory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Default(); err == nil {
		t.Fatal("Default with no providers should error")
	}
	if _, err := r.Get("anything"); err == nil {
		t.Fatal("Get with no providers should error")
	}
}

func TestRegistry_MissingBaseURL(t *testing.T) {
	cfg := config.InferenceConfig{
		Providers: map[string]config.InferenceProviderConfig{
			"x": {APIKey: "k"},
		},
	}
	_, err := inference.NewRegistry(cfg, stubFactory)
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestRegistry_FactoryError(t *testing.T) {
	bad := errors.New("boom")
	failing := func(_ inference.ProviderConfig) (inference.Provider, error) { return nil, bad }
	cfg := config.InferenceConfig{
		Providers: map[string]config.InferenceProviderConfig{
			"x": {BaseURL: "y"},
		},
	}
	_, err := inference.NewRegistry(cfg, failing)
	if !errors.Is(err, bad) {
		t.Fatalf("expected %v, got %v", bad, err)
	}
}

func TestProviderConfig_TimeoutDefault(t *testing.T) {
	captured := make(chan inference.ProviderConfig, 1)
	cfg := config.InferenceConfig{
		Providers: map[string]config.InferenceProviderConfig{
			"x": {BaseURL: "y", RequestTimeoutSecs: 30},
		},
	}
	_, err := inference.NewRegistry(cfg, func(c inference.ProviderConfig) (inference.Provider, error) {
		captured <- c
		return &stubProvider{cfg: c}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := <-captured
	if got.Timeout != 30*time.Second {
		t.Errorf("Timeout: got %v want 30s", got.Timeout)
	}
}
