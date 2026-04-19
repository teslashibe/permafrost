---
sidebar_position: 4
title: Trading Desk UI
---

# Trading Desk UI

The arctic-themed operator dashboard. React + Vite, no UI framework dependency. Hand-authored SVG sprites for every character (see [the cast](/brand/cast)).

v1 ships as a **read-only** view backed by 3-second polling against the daemon's REST surface.

## Run it

```bash
cd apps/desk
npm install
npm run dev          # http://127.0.0.1:5173
```

The Vite dev server proxies `/v1/*` to `http://127.0.0.1:8080` (the permafrostd default), so the UI works against `make up` without you having to configure CORS.

If the daemon isn't reachable, the UI falls back to **demo mode** — a small canned dataset (Pip + Boulder example agents) so you can preview the experience without `make up`. A yellow "demo mode (daemon unreachable)" banner makes the state unambiguous.

## Build

```bash
cd apps/desk
npm run typecheck    # tsc --noEmit, strict
npm run build        # → dist/, ~56 KB gzipped with all sprites
npm run preview      # serve dist/ on :5173
```

The `dist/` directory is what a future PR will `go:embed` into the daemon and serve under `/ui`.

## Layout (v1)

```
┌──────────────────────────────────────────────────────────────┐
│  [Pole] Camp Director · Captain Pole         [conn] [Owl]    │
├──────────────────┬───────────────────────────────────────────┤
│  Vault $1,500    │  Decision Log                             │
│  🪙🪙🪙🪙🪙       │  ─ 8s   noop                  [bar]       │
│                  │  ─ 12s  dca buy: 50 USDC      [bar]       │
│  Agents          │  …                                        │
│  ─ Pip 🐧+🦄     │                                           │
│  ─ Boulder 🐧    │                                           │
├──────────────────┴───────────────────────────────────────────┤
│  The Expedition (8-card cast showcase)                       │
└──────────────────────────────────────────────────────────────┘
```

## Sprite states

The CSS animations are driven from `apps/desk/src/styles/global.css`. They never re-render through React.

| Class | Effect | When it fires |
|---|---|---|
| `.shimmer` | Coin highlight pulses (1.6s ease) | Always (vault) |
| `.glow-active .glow` | Narwhal horn brightens | Future: when an LLM round-trip is in flight |
| `.alert .eye` | Owl eye blinks red | Any agent in `halted` or `error` state |

## Connection model

- Vite dev-proxy: `/v1/*` → `http://127.0.0.1:8080`. No CORS config needed.
- Polling cadence: 3 seconds. `listAgents` + `recentDecisions(firstAgent)`.
- On any fetch error: silently fall back to `demoData`, flip the connection dot to yellow, show a footer with the underlying error.

The UI never blank-screens. If the daemon is offline you still see a working dashboard with the demo data.

## What's NOT in v1 (deferred to v2)

- WebSocket / SSE multiplex for sub-second decision latency
- Per-agent action buttons (start / stop / set-mode / kill)
- NAV history chart (currently only the current value)
- Trade-history view
- Whiteout overlay when killswitch fires (Frostbite the Whale breaching)
- Mobile breakpoints (desktop-first by design — this is an operator tool)
- Light theme

## Dependencies (v1)

The desk app's `package.json`:

| Dep | Why |
|---|---|
| `react`, `react-dom` 18 | UI |
| `vite` 5 + `@vitejs/plugin-react` | dev server + bundler |
| `typescript` 5 + `@types/react*` | strict TS |

That's it. **No UI framework**, no state library, no router. Just React + tightly-scoped inline styles + a small global CSS file for the animations. Total bundle: ~56 KB gzipped with all 8 sprites included.

## Where it lives

- `apps/desk/src/characters/*.svg` — 8 hand-authored sprites + coin
- `apps/desk/src/components/` — `Sprite`, `AgentCard`, `DecisionRow`, `DashboardChrome`, `Vault`, `CastShowcase`
- `apps/desk/src/api/client.ts` — REST client + demo-mode fallback
- `apps/desk/src/styles/global.css` — palette + animations
- `apps/desk/vite.config.ts` — dev-proxy
- `.github/workflows/ci.yml` `desk-ui` job — typecheck + build on every PR

## Next steps

- [The cast](/brand/cast) — who each sprite represents
- [LLM-as-agent](/brand/llm-as-agent) — the thesis behind the narwhals
- [Operations: deployment](/operations/deployment) — production deploys
