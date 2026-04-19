---
sidebar_position: 5
---

# Inference

Permafrost wraps an OpenAI-compatible chat-completion client (`pkg/inference.Provider`) so any strategy can ask an LLM. The framework is opinionated about how the LLM gets used.

## What the LLM does and does not do

- **Does:** veto entries based on event/news context, score candidates, classify sentiment, summarize on-chain context for the operator.
- **Does NOT:** invent orders. Strategies are deterministic Go code; the LLM's output is consumed as a `bool` (veto / not), a number (score), or structured JSON via JSON-Schema mode. Never as raw `OrderIntent`s.

This is enforced at the framework layer: the `Strategy.Decide` return value carries `Orders`, `Swaps`, and `Cancels` — typed Go structs. There is no path for an LLM response to be parsed directly into an order intent.

## Configuring providers

Multiple providers can coexist. The agent picks one at create time via `--inference provider:model`:

```yaml
# config.yaml
inference:
  default: openrouter
  providers:
    openrouter:
      base_url: https://openrouter.ai/api/v1
      api_key_env: OPENROUTER_API_KEY
    ollama:
      base_url: http://localhost:11434/v1
      api_key: ""
```

```bash
permafrost agent create \
    --strategy my_strategy \
    --inference openrouter:anthropic/claude-sonnet-4.5
```

Any base URL that speaks the OpenAI chat-completion protocol works: OpenAI, OpenRouter, Groq, Together, Fireworks, vLLM, Ollama, LM Studio, etc.

## How strategies use it

Strategies receive the configured provider via `WarmupInput.Services.Inference`. They store it on the receiver and call `Provider.Complete(ctx, req)` from `Decide`. See [Services](/strategies/services) for the full pattern.

A typical veto path:

```go
resp, err := s.inference.Complete(ctx, inference.Request{
    Model:  s.cfg.VetoModel,
    System: vetoSystemPrompt,
    Messages: []inference.Message{
        {Role: inference.RoleUser, Content: prompt},
    },
    JSONSchema: &inference.Schema{
        Name:   "veto_decision",
        JSON:   []byte(vetoSchemaJSON),
        Strict: true,
    },
    Temperature: 0,
    MaxTokens:   200,
})
```

## Provenance

Every inference request and response is persisted alongside the resulting `Decision`. From the CLI:

```bash
permafrost agent decisions <id> --with-prompts
```

This is what "auditable AI" means in practice — you can see exactly what the model saw, how it responded, and which on-chain actions followed.

## Errors and graceful degradation

`pkg/inference` exposes two sentinel errors strategies should handle:

- `ErrUnsupportedFeature` — provider/model can't do something the request asked for (e.g. JSON Schema on a base Ollama model). Strategies should fall back to a simpler prompt or skip the LLM hop.
- `ErrRateLimited` — provider returned 429. Strategies should default to safe behaviour (e.g. don't open new positions this tick).

A `Provider` that returns errors does **not** crash the runtime — `Strategy.Decide` returns whatever decision it made (often an empty one, with the error in `Notes`).

## Next steps

- [Strategy SAPI](/strategies/sapi)
- [Services](/strategies/services)
