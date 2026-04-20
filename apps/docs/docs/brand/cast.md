---
sidebar_position: 2
title: The Cast
---

# The Cast

Permafrost uses an arctic / polar-expedition metaphor for the system.
Each character represents one piece of the architecture; the visual
language is consistent across the [Trading Desk UI](https://github.com/teslashibe/permafrost/tree/main/apps/desk),
the docs, and the CLI's flavour text.

The visual style is deliberately playful -- hand-authored pixel art,
MarioKart-inspired but Nintendo-IP-free. Cute, distinct, immediately
readable.

## The leadership

### <img src="/img/brand/pole.svg" alt="Pole the polar bear" width="64" height="64" style={{verticalAlign: 'middle'}} /> Pole the polar bear (Captain)

The **Camp Director** -- your avatar. Pole presides over the trading
desk, holds the keystore, sets allocations, picks strategies, and
calls the killswitch. Top of the hierarchy.

In the UI, Pole sits in the top-left of the dashboard chrome,
permanently. He's the one constant on every screen.

### <img src="/img/brand/owl.svg" alt="Aurora the snowy owl" width="64" height="64" style={{verticalAlign: 'middle'}} /> Aurora the snowy owl (Risk Monitor)

Aurora perches in the top-right of the dashboard chrome and watches
the circuit breakers. When a breaker trips -- drawdown, daily loss,
funding flip, RPC degradation -- her eyes blink red and the operator
knows something is wrong before they read the breaker name.

She doesn't *fix* anything; she just sees everything.

## The traders

### <img src="/img/brand/penguin.svg" alt="Penguin trader" width="64" height="64" style={{verticalAlign: 'middle'}} /> Penguin traders

One penguin per running agent. Penguins quote, hedge, execute. Each
penguin has a coloured scarf to distinguish it from its peers (Pip
gets aurora-cyan, Boulder gets warm-orange, etc).

Pip is the canonical first agent (the demo wizard creates one on
first run). Boulder is the canonical second agent (`dca_buy`-style
patient accumulation).

### <img src="/img/brand/narwhal.svg" alt="Narwhal advisor" width="64" height="64" style={{verticalAlign: 'middle'}} /> Narwhal advisors (LLM)

A narwhal floats beside any penguin whose strategy uses inference.
When the LLM is being consulted, the narwhal's horn glows; when it
returns a veto, the horn flashes brighter.

The narwhal is the *visual* form of the [LLM-as-agent](./llm-as-agent)
thesis: inference is a participant in the decision loop, not a
standalone module bolted on the side.

## The support staff

### <img src="/img/brand/husky.svg" alt="Skipper the husky" width="64" height="64" style={{verticalAlign: 'middle'}} /> Skipper the husky (Reconciliation)

Runs between camps delivering reconcile passes -- comparing the
framework's view of positions and balances against what the venues
actually report. Catches drift, missed fills, partial swaps.

### <img src="/img/brand/walrus.svg" alt="Kelp the walrus" width="64" height="64" style={{verticalAlign: 'middle'}} /> Kelp the walrus (Swap Router)

Hauls tokens between chains via the configured DEX aggregators
(Jupiter on Solana, 1inch on EVM). Big, dependable, can carry a lot.

## The hazards

### <img src="/img/brand/whale.svg" alt="Frostbite the Whale" width="64" height="64" style={{verticalAlign: 'middle'}} /> Frostbite the Whale (Killswitch)

A killer whale. Literal: a "killer whale" is the killswitch.

Frostbite spends most of his life out of sight, somewhere under the
ice. When he surfaces, the expedition is over -- he cancels every open
order, flattens every short, and (if configured) liquidates every
spot leg back to USDC.

You hope to never see Frostbite surface. When you do, the right
response is to read the killswitch reason and figure out what
you missed.

The full-screen "Whiteout" overlay (UI v2) is Frostbite breaching.

### <img src="/img/brand/mammoth.svg" alt="Tusk the mammoth" width="64" height="64" style={{verticalAlign: 'middle'}} /> Tusk the mammoth (Private Strategies)

Tusk is *extinct from public view*. The maintainer's gitignored
strategies under `strategies/private/` are mammoths -- present in
the local build, invisible to the upstream. The visual is a
permanent reminder that the open-source repo intentionally doesn't
ship every strategy in production.

If you're reading this on the public docs site, you'll never see a
mammoth instance in the dashboard. If you're reading this on the
maintainer's local build, you might.

## The currency

### <img src="/img/brand/coin.svg" alt="Coin" width="32" height="32" style={{verticalAlign: 'middle'}} /> Coins

One coin ≈ $100 of accumulated NAV. The vault panel renders coins
stacked at the bottom; they shimmer when added.

The coin metaphor predates the rest of the cast -- it was always
going to be there. The arctic theme just gave it a more concrete
home.

## Why this matters

Trading is dense, fast, and unforgiving. The mental load on the
operator is enormous -- there are dozens of breakers, every venue
has a different latency profile, the LLM is sometimes saying
something useful and sometimes filling air. Names and faces compress
that load.

\"Pip's narwhal vetoed at 14:32\" is one sentence and it tells you:
- which agent (Pip)
- that the agent uses inference (narwhal)
- what happened (veto)
- when (14:32)

without any of those words being technical jargon. That's the
whole point.
