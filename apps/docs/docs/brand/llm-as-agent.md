---
sidebar_position: 1
title: LLM-as-agent
---

# LLM-as-agent

Permafrost is built on a thesis: the next generation of trading systems
will treat the LLM not as a feature, but as an **agent** — a first-class
participant in the decision loop, with structured input, structured
output, and structured accountability.

## What that means

A traditional algorithmic trading bot has a deterministic policy:
> "if funding > 50bps and basis > 30bps then short."

A Permafrost strategy can have a deterministic policy *and* a soft
veto from an LLM that has read the news, the whale flows, the
policy-maker schedule, and the funding history of the past 30 days.

```
strategy.Decide(input)
   │
   ├─ deterministic check ─────► OK to enter
   │
   └─ LLM advisor: "any reason to skip?" ──► JSON {veto:false, reason:"..."}
                                            │
                                       enter trade
```

The LLM doesn't *make* the trade. It vetoes the trade. That's a much
narrower, much more debuggable role than "ask the AI what to do."

## Why this is the right shape

- **Decision provenance.** Every order links back to (input, prompt,
  model_id, response). You can replay the LLM call after the fact. No
  black boxes.
- **JSON-schema enforcement.** The LLM response is constrained by the
  provider (OpenRouter / OpenAI / Groq) at the token level. No
  hallucinated fields, no \"sure, here's a trade\" prose response when
  you needed a boolean.
- **Bounded blast radius.** If the LLM is wrong, the worst case is a
  missed trade. The LLM is never the only thing between the strategy
  and the venue.
- **Cost-controlled.** A veto is one round-trip per refresh cycle.
  At a 60-second tick rate, that's $0.005 per cycle on a cheap model
  — meaningful but not ruinous.

## Why other shapes fail

**\"Let the LLM generate the trade.\"** Latency and reliability fall
off a cliff. Costs explode. Provenance becomes \"you asked the model
and it made up a number.\" When the trade is wrong, the post-mortem
is unproductive.

**\"Use the LLM for sentiment scoring only.\"** Strictly weaker than
the veto pattern: a sentiment score still has to be turned into a
decision rule, and the rule was already what you had.

**\"Train a custom model on your historical trades.\"** Fine, but
that's an ML pipeline, not an LLM. Permafrost supports both — your
custom model can speak the same JSON-schema veto interface as a
hosted LLM. (See [`pkg/inference`](https://github.com/teslashibe/permafrost/tree/main/pkg/inference).)

## The expedition metaphor

We use an arctic / polar-expedition metaphor for the system because
the literal Permafrost name is begging for it. The operator is the
**Camp Director** ([Pole the polar bear](./cast)). The strategies are
**penguin traders** working the ice. Each penguin that uses LLM
inference has a **narwhal advisor** floating beside it, whose horn
glows when the LLM is being consulted.

The metaphor is intentionally a little playful. Trading is a serious
field but it's not a sacred one — and the more concrete the mental
model, the easier it is to onboard new operators. Pip the penguin
quoted a bid; Pip's narwhal said \"funding flip in 30 seconds, skip\";
Pip didn't quote. That's the whole story, told in five seconds.

See [the cast](./cast) for the full character roster.
