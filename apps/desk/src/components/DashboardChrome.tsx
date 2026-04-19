import React from 'react';
import { Sprite } from './Sprite';

// DashboardChrome is the persistent top bar: Pole on the left
// (operator avatar), Aurora the owl on the right (risk monitor — turns
// red on a breaker trip), title in the middle.
//
// alertActive=true triggers Aurora's eye-blink animation via the
// .alert class on her wrapper.

export interface DashboardChromeProps {
  title: string;
  subtitle?: string;
  alertActive?: boolean;
  connected: boolean;
}

export const DashboardChrome: React.FC<DashboardChromeProps> = ({
  title, subtitle, alertActive = false, connected,
}) => (
  <header
    style={{
      display: 'grid',
      gridTemplateColumns: 'auto 1fr auto',
      alignItems: 'center',
      padding: '12px 24px',
      gap: 16,
      borderBottom: '1px solid var(--ice-edge)',
      background: 'rgba(10,27,54,0.85)',
      backdropFilter: 'blur(6px)',
      position: 'sticky',
      top: 0,
      zIndex: 10,
    }}
  >
    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
      <Sprite name="pole" size={56} />
      <div>
        <div style={{ fontSize: 12, opacity: 0.7, letterSpacing: 0.5, textTransform: 'uppercase' }}>
          Camp Director
        </div>
        <div style={{ fontSize: 14, fontWeight: 600 }}>Captain Pole</div>
      </div>
    </div>

    <div style={{ textAlign: 'center' }}>
      <h1 style={{ margin: 0, fontSize: 18 }}>{title}</h1>
      {subtitle && <div style={{ fontSize: 11, opacity: 0.7 }}>{subtitle}</div>}
    </div>

    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
      <div style={{
        display: 'flex', alignItems: 'center', gap: 6,
        fontSize: 11, opacity: 0.8,
      }}>
        <span style={{
          width: 8, height: 8, borderRadius: '50%',
          background: connected ? 'var(--aurora-c)' : 'var(--warning)',
          boxShadow: connected ? '0 0 8px var(--aurora-c)' : 'none',
        }}/>
        {connected ? 'connected' : 'demo mode'}
      </div>
      <div className={alertActive ? 'alert' : ''}>
        <Sprite name="owl" size={56} title={alertActive ? 'Aurora is alert — a breaker tripped' : 'Aurora is watching'} />
      </div>
    </div>
  </header>
);
