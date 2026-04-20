# Permafrost -- Trading Desk

The arctic-themed operator dashboard. React + Vite, no UI framework
dependency. Hand-authored SVG sprites for every character (see
`src/characters/`).

## Run locally

```bash
cd apps/desk
npm install
npm run dev          # http://127.0.0.1:5173
```

The dev server proxies `/v1/*` to `http://127.0.0.1:8080` (the
permafrostd default) so the UI works against `make up` without you
having to configure CORS.

If the daemon isn't reachable, the UI falls back to a small **demo
mode** dataset so you can preview the experience without `make up`.

## Build

```bash
npm run typecheck    # tsc --noEmit
npm run build        # → dist/
npm run preview      # serve dist/ on :5173
```

The `dist/` directory is what a future PR will `go:embed` into the
daemon and serve under `/ui`.

## Cast

| Character                 | Role                                      |
|---------------------------|-------------------------------------------|
| 🐻 Pole the polar bear    | Camp Director -- the operator's avatar.    |
| 🐧 Penguin trader         | One per running agent.                    |
| 🦄 Narwhal advisor        | Floats beside an agent that uses the LLM. |
| 🦉 Aurora the snowy owl   | Risk monitor; eye blinks red on a trip.   |
| 🐕 Skipper the husky      | Reconciliation runner.                    |
| 🦦 Kelp the walrus        | Swap router.                              |
| 🐳 Frostbite the Whale   | Killswitch.                               |
| 🦣 Tusk the mammoth       | Operator's gitignored private strategies. |
| 🪙 Coin                   | One per ~$100 of accumulated NAV.         |

See [the cast page](../docs/docs/brand/cast.md) and the
[LLM-as-agent thesis](../docs/docs/brand/llm-as-agent.md) for the
full metaphor.

## v1 scope

- Static layout: chrome (Pole + Aurora) + Vault + Agents rail + Decision Log + Cast showcase
- 3-second polling against `/v1/agents` and `/v1/agents/<id>/decisions`
- Demo-mode fallback when the daemon is offline
- All sprites and CSS animations included; build is < 60 KB gzipped

## Out of scope (v2)

- WebSocket / SSE multiplex for sub-second decision latency
- Per-agent action buttons (start / stop / set-mode / kill)
- NAV history chart (currently just the current value)
- Trade-history view
- Dark/light mode (we're dark-only on purpose for v1)
- Mobile breakpoints (desktop-first; mobile in v2)
