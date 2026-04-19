import React from 'react';
import { Agent } from '../api/client';
import { Sprite } from './Sprite';

// AgentCard renders one running agent as a panel. The penguin's scarf
// is colour-coded by status; a narwhal floats beside if the strategy
// uses inference (heuristic: market_maker_basic + funding_arb_basic).
//
// Cards stack vertically in the dashboard's left rail.

const STATUS_COLOUR: Record<Agent['status'], string> = {
  idle:    'var(--ice-edge)',
  running: 'var(--aurora-c)',
  halted:  'var(--warning)',
  error:   'var(--danger)',
};

const STRATEGIES_WITH_LLM = new Set(['market_maker_basic', 'funding_arb_basic']);

export const AgentCard: React.FC<{ agent: Agent }> = ({ agent }) => {
  const usesLLM = STRATEGIES_WITH_LLM.has(agent.strategy);
  return (
    <div
      style={{
        background: 'var(--ice-mid)',
        border: `1px solid ${STATUS_COLOUR[agent.status]}`,
        borderRadius: 8,
        padding: 12,
        display: 'grid',
        gridTemplateColumns: '64px 1fr auto',
        gap: 12,
        alignItems: 'center',
      }}
    >
      <div style={{ position: 'relative', width: 64, height: 64 }}>
        <Sprite name="penguin" size={64} />
        {usesLLM && (
          <div style={{ position: 'absolute', right: -42, top: 8 }}>
            <Sprite name="narwhal" size={48} />
          </div>
        )}
      </div>

      <div style={{ marginLeft: usesLLM ? 50 : 0 }}>
        <div style={{ fontSize: 14, fontWeight: 600 }}>
          {agent.name || agent.id}
        </div>
        <div style={{ fontSize: 11, opacity: 0.7 }}>
          {agent.strategy} · {agent.perp_venue}{agent.spot_venue ? ` · ${agent.spot_venue}` : ''} · {agent.network}
        </div>
        <div style={{ fontSize: 10, marginTop: 4, opacity: 0.5 }}>
          ${agent.alloc_usd} alloc · tick {agent.tick_secs}s
        </div>
      </div>

      <div
        style={{
          padding: '4px 8px',
          background: STATUS_COLOUR[agent.status],
          color: 'var(--ice-deep)',
          borderRadius: 4,
          fontSize: 10,
          fontWeight: 600,
          textTransform: 'uppercase',
          letterSpacing: 0.5,
        }}
      >
        {agent.mode} · {agent.status}
      </div>
    </div>
  );
};
