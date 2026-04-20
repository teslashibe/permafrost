import React, { useEffect, useState } from 'react';
import { APIClient, Agent, DecisionLite, demoData, nextDemoBatch } from './api/client';
import { World } from './components/World';
import { VaultHud, AgentLegendHud, DecisionLogHud, CastHud } from './components/Hud';
import { Sprite } from './components/Sprite';
import { resetLayout } from './hooks/useDraggable';

// App is the root view. The world fills the viewport; HUDs (Vault,
// Agent legend, Decision log, Cast) overlay the corners. Title +
// connection state pin to the top.

const POLL_MS = 3000;
const client = new APIClient();

export const App: React.FC = () => {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [decisions, setDecisions] = useState<DecisionLite[]>([]);
  const [connected, setConnected] = useState(false);
  const [lastError, setLastError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const poll = async () => {
      try {
        const list = await client.listAgents();
        if (cancelled) return;
        setAgents(list);
        setConnected(true);
        setLastError(null);
        if (list.length > 0) {
          // Pull recent decisions from EVERY agent so the World can
          // animate each penguin independently. Limit per-agent so a
          // chatty strategy doesn't overwhelm the view.
          const all = await Promise.all(
            list.map(a => client.recentDecisions(a.id, 5).catch(() => [] as DecisionLite[]))
          );
          if (!cancelled) {
            const merged = all.flat().sort((x, y) =>
              new Date(y.ts).getTime() - new Date(x.ts).getTime()
            );
            setDecisions(merged.slice(0, 30));
          }
        } else {
          setDecisions([]);
        }
      } catch (err) {
        if (cancelled) return;
        setConnected(false);
        setAgents(demoData.agents);
        setDecisions(demoData.recent_decisions);
        setLastError(err instanceof Error ? err.message : String(err));
      } finally {
        if (!cancelled) timer = setTimeout(poll, POLL_MS);
      }
    };
    void poll();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, []);

  // While disconnected, advance the demo scene every 4s so the world
  // keeps animating instead of freezing on the canned snapshot.
  useEffect(() => {
    if (connected) return;
    const t = setInterval(() => {
      setDecisions(prev => nextDemoBatch(prev));
    }, 4_000);
    return () => clearInterval(t);
  }, [connected]);

  // Synthesised NAV (sum of allocations) until /v1/vault/nav lands.
  const navUSD = agents.reduce((acc, a) => acc + parseFloat(a.alloc_usd || '0'), 0);
  const alertActive = agents.some(a => a.status === 'halted' || a.status === 'error');

  return (
    <>
      {/* Top chrome -- title + connection dot */}
      <header className="chrome">
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <Sprite name="pole" size={48} />
          <div style={{ fontSize: 11, opacity: 0.7, letterSpacing: 0.5, textTransform: 'uppercase' }}>
            Camp Director
          </div>
          <div style={{ fontSize: 13, fontWeight: 600 }}>Pole the Polar Bear</div>
        </div>
        <div className="title">
          <h1>Permafrost - Trading Desk</h1>
          <div className="subtitle">
            {connected ? 'live data from permafrostd' : 'demo mode (daemon unreachable)'}
          </div>
        </div>
        <div className="right">
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 11, opacity: 0.85 }}>
            <span className={`conn-dot ${connected ? 'live' : ''}`} />
            {connected ? 'connected' : 'demo'}
          </div>
        </div>
      </header>

      {/* The animated arctic world */}
      <World agents={agents} decisions={decisions} alertActive={alertActive} />

      {/* HUD overlays */}
      <VaultHud navUSD={navUSD} />
      <AgentLegendHud agents={agents} />
      <DecisionLogHud decisions={decisions} />
      <CastHud />

      {/* Reset-layout escape hatch -- if a HUD or sprite ends up in
          an unrecoverable position (or the user just wants to
          start over), one click clears every persisted position
          and reloads. Pinned discreetly to the top-right edge,
          beside the connection dot. */}
      <button
        type="button"
        onClick={() => {
          if (confirm('Reset all HUD and sprite positions to defaults?')) {
            resetLayout();
          }
        }}
        title="Reset all draggable HUD and sprite positions"
        style={{
          position: 'fixed', top: 20, right: 92, zIndex: 60,
          background: 'rgba(10,27,54,0.7)', border: '1px solid var(--ice-edge)',
          color: 'var(--ice-bright)', fontSize: 11, fontFamily: 'inherit',
          padding: '4px 8px', borderRadius: 6, cursor: 'pointer',
          opacity: 0.65, transition: 'opacity 120ms ease-out',
        }}
        onMouseEnter={e => (e.currentTarget.style.opacity = '1')}
        onMouseLeave={e => (e.currentTarget.style.opacity = '0.65')}
      >
        ↺ reset layout
      </button>

      {/* Footer error banner -- only when offline AND we have an error */}
      {lastError && !connected && (
        <div className="disconnect-footer">
          {lastError.length > 90 ? lastError.slice(0, 87) + '...' : lastError}
          {' - run '}<code>make up</code>{' for live data'}
        </div>
      )}
    </>
  );
};
