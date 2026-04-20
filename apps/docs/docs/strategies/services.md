---
sidebar_position: 3
---

# Services

`Services` is the bag of framework-provided dependencies that strategies receive in `WarmupInput`. It's the canonical extension point for new framework features that strategies may depend on -- add fields here rather than expanding `WarmupInput` or `DecisionInput`.

## The struct

```go
type Services struct {
    // Logger is an agent-scoped slog logger. Always non-nil.
    Logger *slog.Logger

    // Inference is the resolved LLM Provider for this agent. nil when
    // the operator did not configure agent.Inference.
    Inference inference.Provider

    // InferenceModel is the model id parsed from agent.Inference (the
    // part after ":"). Surfaced so strategies can use the operator's
    // chosen model as a default and override via their own typed cfg.
    InferenceModel string

    // Registry is the curated asset registry. Always non-nil when set
    // by BuildDeps; the embedded copy is loaded on every BuildDeps call.
    Registry assets.Registry
}
```

## How to use it

Pull the services you need in `Warmup`. Validate that required ones are present; return an error if not.

```go
type Strategy struct {
    cfg       Config
    inference inference.Provider // captured in Warmup
    log       *slog.Logger
}

func (s *Strategy) Warmup(_ context.Context, in strategy.WarmupInput) error {
    s.log = in.Services.Logger
    s.inference = in.Services.Inference

    // Use the operator's chosen model as a default if the typed cfg
    // didn't override it.
    if s.cfg.Model == "" {
        s.cfg.Model = in.Services.InferenceModel
    }

    if s.cfg.UseLLMVeto && s.inference == nil {
        return fmt.Errorf("my_strategy: use_llm_veto=true but no inference provider configured for agent")
    }
    return nil
}
```

`Warmup` is once-guarded by the runtime -- even if a foreground CLI drives ticks via `TickOnce` instead of `Start`, the framework calls `Warmup` exactly once before the first decision. Authors can rely on Warmup having completed.

The `Warmup` error path matters: validation failures here surface as agent-launch errors, so the operator notices immediately. Without this, the strategy would launch and fail on the first decision tick.

## Why not pass services through `Constructor`?

The `Constructor` signature is:

```go
type Constructor func(cfg map[string]any) (Strategy, error)
```

It deliberately does **not** take services. Two reasons:

1. **The framework can construct a strategy without booting an entire agent runtime.** Tools like the CLI (`strategy list`) or a future strategy validator can instantiate strategies just to introspect them.
2. **Adding new services should not break every strategy's constructor signature.** Putting them on `Services` means the next time the framework adds a service (say, a `Backtester` reference), no existing strategy has to change.

## What goes on `Services` vs what doesn't

Add to `Services`:

- Framework-provided primitives that multiple strategies reasonably want.
- Things that depend on per-agent config (like inference, which is wired from `agent.Inference`).
- Things that need framework-level construction (registries, factories).

Don't add to `Services`:

- Strategy-specific config -- that goes on the typed `Config` struct your `Constructor` parses.
- Per-tick state -- that's `DecisionInput`.
- Things the strategy can build for itself (e.g. an asset registry -- `assets.LoadEmbedded()` is one line).

## Next steps

- [Decision contract](/strategies/decision-contract)
- [Inference](/concepts/inference)
