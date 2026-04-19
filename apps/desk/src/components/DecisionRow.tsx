import React from 'react';
import { DecisionLite } from '../api/client';

// One decision = one row in the right-rail "Decision Log". Compact;
// no per-row sprite (would be too noisy). Confidence rendered as a
// horizontal bar; LLM-used decisions get a subtle aurora-purple tint.

export const DecisionRow: React.FC<{ d: DecisionLite }> = ({ d }) => {
  const ageS = Math.floor((Date.now() - new Date(d.ts).getTime()) / 1000);
  const ageStr = ageS < 60 ? `${ageS}s` : `${Math.floor(ageS / 60)}m`;
  const tint = d.llm_used ? 'rgba(181,166,224,0.08)' : 'transparent';

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: '40px 1fr auto',
        gap: 8,
        padding: '6px 8px',
        borderBottom: '1px solid var(--ice-edge)',
        background: tint,
        fontSize: 11,
      }}
    >
      <div style={{ opacity: 0.6 }}>{ageStr}</div>
      <div>
        <div style={{ marginBottom: 2 }}>{d.notes || '(no notes)'}</div>
        <div style={{ display: 'flex', gap: 8, opacity: 0.6, fontSize: 10 }}>
          <span>{d.num_orders} orders</span>
          <span>{d.num_swaps} swaps</span>
          {d.llm_used && <span style={{ color: 'var(--aurora-p)' }}>llm</span>}
        </div>
      </div>
      <div style={{ width: 40 }}>
        <div style={{
          height: 6,
          width: '100%',
          background: 'var(--ice-deep)',
          borderRadius: 2,
          position: 'relative',
        }}>
          <div style={{
            position: 'absolute', top: 0, left: 0, height: '100%',
            width: `${Math.max(2, Math.min(100, d.confidence * 100))}%`,
            background: 'var(--aurora-c)',
            borderRadius: 2,
          }}/>
        </div>
      </div>
    </div>
  );
};
