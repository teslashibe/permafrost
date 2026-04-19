import React from 'react';
import { Sprite } from './Sprite';

// Vault renders the operator's accumulated PnL as ice cores (coin
// sprites) stacked at the bottom of the panel. v1 ships with a
// static prop; v2 will live-update from the daemon's vault-NAV stream.

export interface VaultProps {
  navUSD: number;
  drawdownPct?: number; // 0..1
}

export const Vault: React.FC<VaultProps> = ({ navUSD, drawdownPct = 0 }) => {
  // Visualise NAV as a coin count (capped to 12 for layout). Each coin
  // = $100 logical unit; below $100 we still show 1 coin. Pure visual.
  const coinCount = Math.max(1, Math.min(12, Math.round(navUSD / 100)));
  const inDrawdown = drawdownPct > 0.05;

  return (
    <section style={{
      padding: 16,
      background: 'var(--ice-mid)',
      border: `1px solid ${inDrawdown ? 'var(--warning)' : 'var(--ice-edge)'}`,
      borderRadius: 8,
    }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
        <h3 style={{ margin: 0, fontSize: 12, letterSpacing: 0.5, textTransform: 'uppercase', opacity: 0.8 }}>
          Vault
        </h3>
        <div style={{ fontSize: 11, opacity: 0.7 }}>
          {drawdownPct > 0 ? `↓ ${(drawdownPct * 100).toFixed(2)}%` : '—'}
        </div>
      </div>
      <div style={{ fontSize: 22, fontWeight: 700, marginBottom: 12 }}>
        ${navUSD.toLocaleString(undefined, { maximumFractionDigits: 2 })}
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
        {Array.from({ length: coinCount }, (_, i) => (
          <Sprite key={i} name="coin" size={28} />
        ))}
      </div>
      <div style={{ fontSize: 10, opacity: 0.5, marginTop: 8 }}>
        Each coin ≈ $100 of accumulated NAV.
      </div>
    </section>
  );
};
