import React from 'react';
import { Agent, DecisionLite } from '../api/client';
import { Sprite } from './Sprite';
import { useDraggable } from '../hooks/useDraggable';

// All four HUD panels are draggable by their <h3> title bar. The
// position is persisted in localStorage under a stable id so it
// survives reloads. See useDraggable() for the details.
//
// Default positions (when localStorage is empty) come from the CSS
// classes (.hud.vault / .hud.legend / .hud.log / .hud.cast).

// DragHandle is the visual affordance on each h3 -- a tiny "grip"
// glyph on the right edge that hints "this thing is draggable". The
// whole h3 is the actual click target.
const DragHandle: React.FC = () => (
  <span
    aria-hidden
    style={{
      float: 'right',
      opacity: 0.5,
      letterSpacing: 1,
      fontSize: 9,
      lineHeight: '12px',
      transform: 'translateY(2px)',
    }}
  >
    {/* unicode "vertical four dots" -- portable across fonts */}
    &#x2807;&#x2807;
  </span>
);

// VaultHud -- top-left coin counter. NAV in big numbers + a coin stack
// that grows as agents earn (visual only; coin count derived from NAV).
export const VaultHud: React.FC<{ navUSD: number; drawdownPct?: number }> = ({
  navUSD, drawdownPct = 0,
}) => {
  const drag = useDraggable('hud:vault');
  const coinCount = Math.max(1, Math.min(16, Math.round(navUSD / 100)));
  const inDrawdown = drawdownPct > 0.05;
  return (
    <div
      ref={drag.ref}
      className="hud vault"
      style={{ ...drag.style, borderColor: inDrawdown ? 'var(--warning)' : undefined }}
    >
      <h3 {...drag.handleProps}>Vault<DragHandle /></h3>
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

// AgentLegendHud -- top-right roster of running agents. One row per
// agent with mini-sprite + name + status badge. Provides the "table"
// view operators expect alongside the immersive world.
const STATUS_COLOUR: Record<Agent['status'], string> = {
  idle:    'var(--ice-edge)',
  running: 'var(--aurora-c)',
  halted:  'var(--warning)',
  error:   'var(--danger)',
};

export const AgentLegendHud: React.FC<{ agents: Agent[] }> = ({ agents }) => {
  const drag = useDraggable('hud:legend');
  return (
    <div ref={drag.ref} className="hud legend" style={drag.style}>
      <h3 {...drag.handleProps}>Camp roster<DragHandle /></h3>
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
};

// DecisionLogHud -- bottom-left scrolling log. Newest at top.
// Fixed height so the panel doesn't grow as more decisions arrive
// (the body scrolls instead).
export const DecisionLogHud: React.FC<{ decisions: DecisionLite[] }> = ({ decisions }) => {
  const drag = useDraggable('hud:log');
  return (
    <div ref={drag.ref} className="hud log" style={drag.style}>
      <h3 {...drag.handleProps}>Decision log<DragHandle /></h3>
      <div className="hud-log-body">
        {decisions.length === 0 && (
          <div style={{ fontSize: 10, opacity: 0.7, padding: '8px 0' }}>
            Waiting for the first decision tick&hellip;
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
                  {d.num_orders} orders &middot; {d.num_swaps} swaps
                  {d.llm_used && <span className="pill llm-pill">llm</span>}
                </div>
              </div>
              <div className="conf"><span style={{ width: `${Math.max(2, Math.min(100, d.confidence * 100))}%` }} /></div>
            </div>
          );
        })}
      </div>
    </div>
  );
};

// CastHud -- bottom-right mini-grid of every character with one-line
// blurb on hover (via title). Compact reference for what each sprite
// represents in the world.
const CAST_ENTRIES: Array<{ name: import('./Sprite').CharacterName; label: string; blurb: string }> = [
  { name: 'pole',    label: 'Pole the Polar Bear', blurb: 'Camp Director (you)' },
  { name: 'penguin', label: 'Penguin', blurb: 'Strategy agent' },
  { name: 'narwhal', label: 'Narwhal', blurb: 'LLM advisor' },
  { name: 'owl',     label: 'Aurora',  blurb: 'Risk monitor' },
  { name: 'husky',   label: 'Skipper', blurb: 'Reconciler' },
  { name: 'walrus',  label: 'Kelp',    blurb: 'Swap router' },
  { name: 'whale',   label: 'Frostbite', blurb: 'Killswitch' },
  { name: 'mammoth', label: 'Tusk',    blurb: 'Private strats' },
];

export const CastHud: React.FC = () => {
  const drag = useDraggable('hud:cast');
  return (
    <div ref={drag.ref} className="hud cast" style={drag.style}>
      <h3 {...drag.handleProps}>The expedition<DragHandle /></h3>
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
};
