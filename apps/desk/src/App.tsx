import React, { useEffect, useState } from 'react';
import { APIClient, Agent, DecisionLite, demoData } from './api/client';
import { DashboardChrome } from './components/DashboardChrome';
import { AgentCard } from './components/AgentCard';
import { DecisionRow } from './components/DecisionRow';
import { Vault } from './components/Vault';
import { CastShowcase } from './components/CastShowcase';

// App is the root view. v1 layout:
//
//   ┌──────────────────────────────────────────────────────────────┐
//   │  [Pole]   ❄ Permafrost — Trading Desk          [conn] [Owl] │  chrome
//   ├──────────────────┬──────────────────────────────────────────┤
//   │                  │                                          │
//   │  Vault           │  Decision Log                            │
//   │  (coins)         │  (last 20)                               │
//   │                  │                                          │
//   │  Agents          │                                          │
//   │  ─ Pip 🐧+🦄     │                                          │
//   │  ─ Boulder 🐧    │                                          │
//   │  ─ ...           │                                          │
//   │                  │                                          │
//   ├──────────────────┴──────────────────────────────────────────┤
//   │  The Expedition (cast showcase)                             │
//   └──────────────────────────────────────────────────────────────┘
//
// Polls /v1/agents and /v1/agents/<id>/decisions every 3s. v2 will
// switch to a single WebSocket multiplex for sub-second latency
// (#41 follow-up; the polling baseline is fine for the v1 ship).

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
        // For v1 just pull decisions for the first agent. v2 will
        // multiplex over WebSocket and merge per-agent streams.
        if (list.length > 0) {
          const decs = await client.recentDecisions(list[0].id, 20);
          if (!cancelled) setDecisions(decs);
        } else {
          setDecisions([]);
        }
      } catch (err) {
        if (cancelled) return;
        // Disconnected: show the demo data so the UI still feels
        // alive, but flag it clearly.
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

  // V1 doesn't yet pipe NAV through the API — synthesise from
  // alloc_usd as a stand-in. Real NAV stream lands when /v1/vault/nav
  // is added (separate small PR).
  const navUSD = agents.reduce((acc, a) => acc + parseFloat(a.alloc_usd || '0'), 0);

  // Aurora alerts when any agent is halted or errored. Cheap heuristic
  // until per-breaker telemetry is plumbed.
  const alertActive = agents.some(a => a.status === 'halted' || a.status === 'error');

  return (
    <>
      <DashboardChrome
        title="Permafrost — Trading Desk"
        subtitle={connected ? 'live data from permafrostd' : 'demo mode (daemon unreachable)'}
        alertActive={alertActive}
        connected={connected}
      />

      <main style={{
        display: 'grid',
        gridTemplateColumns: 'minmax(320px, 1fr) 2fr',
        gap: 16,
        padding: 24,
        maxWidth: 1400,
        margin: '0 auto',
      }}>
        <aside style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          <Vault navUSD={navUSD} />
          <section>
            <h2 style={{ fontSize: 14, opacity: 0.8, letterSpacing: 0.5, marginBottom: 8, textTransform: 'uppercase' }}>
              Agents
            </h2>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              {agents.length === 0
                ? <EmptyAgents />
                : agents.map(a => <AgentCard key={a.id} agent={a} />)}
            </div>
          </section>
        </aside>

        <section>
          <h2 style={{ fontSize: 14, opacity: 0.8, letterSpacing: 0.5, marginBottom: 8, textTransform: 'uppercase' }}>
            Decision Log
          </h2>
          <div style={{
            background: 'var(--ice-mid)',
            border: '1px solid var(--ice-edge)',
            borderRadius: 8,
            overflow: 'hidden',
          }}>
            {decisions.length === 0
              ? <EmptyDecisions connected={connected} />
              : decisions.map(d => <DecisionRow key={d.id} d={d} />)}
          </div>
        </section>
      </main>

      <CastShowcase />

      {lastError && !connected && (
        <footer style={{ padding: 12, fontSize: 10, opacity: 0.5, textAlign: 'center' }}>
          {lastError} — start the daemon with <code>make up</code> to see live data.
        </footer>
      )}
    </>
  );
};

const EmptyAgents: React.FC = () => (
  <div style={{
    padding: 16, background: 'var(--ice-mid)',
    border: '1px dashed var(--ice-edge)', borderRadius: 8,
    fontSize: 12, opacity: 0.7,
  }}>
    No agents yet. Try <code>permafrost agent create --strategy noop --perp hyperliquid --alloc 1000</code>.
  </div>
);

const EmptyDecisions: React.FC<{ connected: boolean }> = ({ connected }) => (
  <div style={{ padding: 24, fontSize: 12, opacity: 0.7, textAlign: 'center' }}>
    {connected
      ? 'Waiting for the first decision tick…'
      : 'No decisions to show. Start the daemon to see live activity.'}
  </div>
);
