---
sidebar_position: 5
---

# Prerequisites

Permafrost runs locally. You need:

- **Go 1.25+** ([install](https://go.dev/doc/install)). Verify with `go version`.
- **Docker** + **docker-compose**. Used to run TimescaleDB; not strictly required if you can supply your own Postgres.
- **git**.

## Optional but recommended

- **A Hyperliquid account** with API access. You can run paper-mode without one (funding-only reads), but live mode needs a Hyperliquid signer in the keystore.
- **A Solana RPC URL.** Public defaults exist but rate-limit aggressively; for live trading use a paid provider (Helius, Triton, etc.).
- **An OpenAI-compatible LLM endpoint.** Required only if any of your strategies use the LLM-veto path. Works with OpenAI, OpenRouter, Groq, Together, vLLM, Ollama, LM Studio, etc.

## EVM (optional)

If you want to trade EVM spot legs (Ethereum, Base, Avalanche, BSC), you also need:

- A **1inch API key** ([get one](https://portal.1inch.dev/)).
- An **EVM RPC URL** for each chain you trade. Public RPCs work for testing; production needs a paid provider.

## Verifying your environment

```bash
go version          # ≥ 1.25
docker --version
docker compose version
git --version
```

If any of these are missing, install them before proceeding to [local install](/getting-started/local-install).
