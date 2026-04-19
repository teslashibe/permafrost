import React from 'react';
import { Agent, DecisionLite } from '../api/client';
import { Sprite } from './Sprite';

// VaultHud — top-left coin counter. NAV in big numbers + a coin stack
// that grows as agents earn (visual only; coin count derived from NAV).
export const VaultHud: React.FC<{ navUSD: number; drawdownPct?: number }> = ({
  navUSD, drawdownPct = 0,
}) => {
  const coinCount = Math.max(1, Math.min(16, Math.round(navUSD / 100)));
  const inDrawdown = drawdownPct > 0.05;
  return (
    <div className="hud vault" style={{ borderColor: inDrawdown ? 'var(--warning)' : undefined }}>
      <h3>Vault</h3>
      <div className="nav">${navUSD.toLocaleString(undefined, { maximumFractionDigits: 2 })}</div>
      <div style={{ fontSize: 10, opacity: 0.7, marginBottom: 8 }}>
        {drawdownPct > 0 ? `drawdown -${(drawdownPct * 100).toFixed(2)}%` : 'at high-water mark'}
      </div>
      <div className="coin-stack">
        {Array.from({ length: coinCount }, (_, i) => (
          <Sprite key={i} name="coin" size={20} />
        ))}
      </div>
      <div className="footnote">Each coin ~ $100 NAV.</div>
    </div>
  );
};

// AgentLegendHud — top-right roster of running agents. One row per
// agent with mini-sprite + name + status badge. Provides the "table"
// view operators expect alongside the immersive world.
const STATUS_COLOUR: Record<Agent['status'], string> = {
  idle:    'var(--ice-edge)',
  running: 'var(--aurora-c)',
  halted:  'var(--warning)',
  error:   'var(--danger)',
};

export const AgentLegendHud: React.FC<{ agents: Agent[] }> = ({ agents }) => (
  <div className="hud legend">
    <h3>Camp roster</h3>
    {agents.length === 0 && (
      <div style={{ fontSize: 10, opacity: 0.7, padding: '8px 0' }}>
        No agents yet. <code>permafrost agent create --strategy noop</code>.
      </div>
    )}
    {agents.map(a => (
      <div key={a.id} className="row">
        <Sprite name="penguin" size={28} />
        <div>
          <div className="name">{a.name || a.id.slice(0, 12)}</div>
          <div className="meta">
            {a.strategy} - {a.perp_venue}
            {a.spot_venue ? ` - ${a.spot_venue}` : ''} - ${a.alloc_usd}
          </div>
        </div>
        <div
          className={`badge ${a.status}`}
          style={{ background: STATUS_COLOUR[a.status] }}
        >
          {a.mode}
        </div>
      </div>
    ))}
  </div>
);

// DecisionLogHud — bottom-left scrolling log. Newest at top.
export const DecisionLogHud: React.FC<{ decisions: DecisionLite[] }> = ({ decisions }) => (
  <div className="hud log">
    <h3>Decision log</h3>
    {decisions.length === 0 && (
      <div style={{ fontSize: 10, opacity: 0.7, padding: '8px 0' }}>
        Waiting for the first decision tick…
      </div>
    )}
    {decisions.map(d => {
      const ageS = Math.floor((Date.now() - new Date(d.ts).getTime()) / 1000);
      const ageStr = ageS < 60 ? `${ageS}s` : ageS < 3600 ? `${Math.floor(ageS / 60)}m` : `${Math.floor(ageS / 3600)}h`;
      return (
        <div key={d.id} className={`row ${d.llm_used ? 'llm' : ''}`}>
          <div className="age">{ageStr}</div>
          <div>
            <div>{d.notes || '(no notes)'}</div>
            <div style={{ fontSize: 9, opacity: 0.6 }}>
              {d.num_orders} orders · {d.num_swaps} swaps
              {d.llm_used && <span className="pill llm-pill">llm</span>}
            </div>
          </div>
          <div className="conf"><span style={{ width: `${Math.max(2, Math.min(100, d.confidence * 100))}%` }} /></div>
        </div>
      );
    })}
  </div>
);

// CastHud — bottom-right mini-grid of every character with one-line
// blurb on hover (via title). Compact reference for what each sprite
// represents in the world.
const CAST_ENTRIES: Array<{ name: import('./Sprite').CharacterName; label: string; blurb: string }> = [
  { name: 'pole',    label: 'Pole',    blurb: 'Camp Director (you)' },
  { name: 'penguin', label: 'Penguin', blurb: 'Strategy agent' },
  { name: 'narwhal', label: 'Narwhal', blurb: 'LLM advisor' },
  { name: 'owl',     label: 'Aurora',  blurb: 'Risk monitor' },
  { name: 'husky',   label: 'Skipper', blurb: 'Reconciler' },
  { name: 'walrus',  label: 'Kelp',    blurb: 'Swap router' },
  { name: 'kraken',  label: 'Frostbite', blurb: 'Killswitch' },
  { name: 'mammoth', label: 'Tusk',    blurb: 'Private strats' },
];

export const CastHud: React.FC = () => (
  <div className="hud cast">
    <h3>The expedition</h3>
    <div className="grid">
      {CAST_ENTRIES.map(c => (
        <div key={c.name} className="cell" title={c.blurb}>
          <Sprite name={c.name} size={40} />
          <div>{c.label}</div>
        </div>
      ))}
    </div>
  </div>
);
