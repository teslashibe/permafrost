# Security policy

Permafrost is a self-custodied trading framework. Bugs in the codebase
can put real funds at risk. We take vulnerability reports seriously
and want to make it easy to share them privately.

## Supported versions

| Version | Supported |
|--|--|
| `0.1.x` (latest minor) | yes |
| pre-`0.1.0` | no |

When `0.2.0` lands, the previous minor (`0.1.x`) will receive security
fixes for 90 days. We do not currently backport to two-minors-old.

## How to report a vulnerability

**Please do not file a public GitHub issue for security bugs.** Use
one of:

1. **GitHub private vulnerability disclosure** — go to the
   [Security tab](https://github.com/teslashibe/permafrost/security/advisories/new)
   on this repo and "Report a vulnerability". This is the preferred
   channel.
2. **Email** — `security@teslashibe.dev`. Include a clear repro,
   affected versions, and the expected vs actual behaviour. Encrypted
   mail welcome; PGP key on request.

We aim to acknowledge new reports within **72 hours**. For
critical-severity issues (loss of funds, key disclosure, RCE in
`permafrostd`) we'll engage immediately.

## What's in scope

- The `permafrost` CLI and `permafrostd` daemon
- The strategy SAPI (`pkg/strategy`, `pkg/types`, `pkg/inference`)
- The keystore (`internal/wallet`)
- The killswitch and risk gates (`internal/risk`, `internal/agent/killswitch.go`)
- The exchange and swap adapters (`internal/exchange/*`, `internal/swap/*`)
- The Trading Desk UI under `apps/desk` when served by the daemon

## What's out of scope

- **Operator key compromise.** If your machine, shell history, or
  keystore passphrase is exfiltrated through means unrelated to this
  codebase, that's an operational issue, not a Permafrost vulnerability.
- **Third-party RPC providers.** A misbehaving Solana RPC, 1inch
  upstream, or Hyperliquid endpoint is the provider's problem; report
  it to them. Permafrost wraps these but doesn't underwrite them.
- **LLM provider behaviour.** OpenRouter / OpenAI / Groq output is
  treated as untrusted input; if you can get an LLM to recommend a bad
  trade, that's a strategy-author concern, not a framework
  vulnerability. (Strategy `Decide` is bounded by the framework's risk
  gates; reports about those gates being bypassable are in scope.)
- **Denial of service against your own daemon** by your own
  configuration (e.g. setting `tick_secs: 1` and overwhelming an RPC).
- **Vulnerabilities in pre-`0.1.0` builds.** Upgrade.

## Disclosure timeline

We follow coordinated disclosure:

1. You report privately via one of the channels above.
2. We acknowledge within 72 hours and triage severity.
3. We work with you on a fix, with credit in the resulting advisory
   unless you prefer to remain anonymous.
4. Once a fix ships, we publish a GitHub Security Advisory and (for
   severe issues) a CVE.

If a vulnerability is being actively exploited or has already been
disclosed elsewhere, contact us anyway — we'll move faster on a
mitigation.

## Hardening recommendations for operators

These aren't framework bugs; they're operator hygiene that materially
reduces blast radius if a framework bug ever does surface:

- Run `permafrostd` as a dedicated, non-root user.
- Bind the daemon HTTP API to `127.0.0.1` (the default). If you must
  expose it remotely, set `server.auth_token` and front it with TLS.
- Keep the keystore file at mode `0600` (the default; verify with
  `permafrost wallet path`).
- Source `~/.permafrost/env` only in shells that need it; never check
  it into source control (`.gitignore` already covers it, but double
  check on a fresh clone).
- Use a hardware wallet or KMS for production keys when possible.
  v0.1.0 supports a local encrypted keystore only; KMS is a planned
  v0.2 feature.
- Monitor `permafrost agent decisions <id>` and `permafrost pnl`
  output; the killswitch (`permafrost agent kill <id>
  --liquidate-spot`) is the manual escape hatch.

Thank you for helping keep Permafrost operators safe.
