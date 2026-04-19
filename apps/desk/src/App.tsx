import React, { useEffect, useState } from 'react';
import { APIClient, Agent, DecisionLite, demoData, nextDemoBatch } from './api/client';
import { World } from './components/World';
import { VaultHud, AgentLegendHud, DecisionLogHud, CastHud } from './components/Hud';
import { Sprite } from './components/Sprite';

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
          <Sprite name="pole" size={36} />
          <div style={{ fontSize: 11, opacity: 0.7, letterSpacing: 0.5, textTransform: 'uppercase' }}>
            Camp Director
          </div>
          <div style={{ fontSize: 13, fontWeight: 600 }}>Captain Pole</div>
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
