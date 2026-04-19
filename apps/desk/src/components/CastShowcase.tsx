import React from 'react';
import { Sprite, CharacterName } from './Sprite';

// CastShowcase is an at-a-glance roster of the arctic cast. Lives at
// the bottom of the dashboard so operators (and visitors) can build
// the mental model of who-does-what at a glance.

const CAST: Array<{ name: CharacterName; role: string; blurb: string }> = [
  { name: 'pole',    role: 'Camp Director', blurb: 'You. Sets allocation, picks strategies, calls the killswitch.' },
  { name: 'penguin', role: 'Trader',        blurb: 'One per running agent. Quotes, hedges, executes.' },
  { name: 'narwhal', role: 'LLM Advisor',   blurb: 'Whispers to a penguin when the strategy uses inference.' },
  { name: 'owl',     role: 'Risk Monitor',  blurb: 'Watches breakers. Blinks red on a trip.' },
  { name: 'husky',   role: 'Reconciler',    blurb: 'Runs reconcile passes between camps.' },
  { name: 'walrus',  role: 'Swap Router',   blurb: 'Hauls tokens between chains via DEX aggregators.' },
  { name: 'kraken',  role: 'Killswitch',    blurb: 'Sleeps deep. Wakes on a Whiteout. Run.' },
  { name: 'mammoth', role: 'Private Strat', blurb: 'Visible only on the maintainer\'s build. Gitignored.' },
];

export const CastShowcase: React.FC = () => (
  <section style={{ padding: 24 }}>
    <h2 style={{ fontSize: 14, opacity: 0.8, letterSpacing: 0.5, marginBottom: 16, textTransform: 'uppercase' }}>
      The Expedition
    </h2>
    <div style={{
      display: 'grid',
      gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))',
      gap: 16,
    }}>
      {CAST.map((c) => (
        <div key={c.name} style={{
          background: 'var(--ice-mid)',
          border: '1px solid var(--ice-edge)',
          borderRadius: 8,
          padding: 12,
          display: 'grid',
          gridTemplateColumns: '64px 1fr',
          gap: 12,
          alignItems: 'center',
        }}>
          <Sprite name={c.name} size={64} />
          <div>
            <div style={{ fontSize: 12, fontWeight: 600 }}>{c.role}</div>
            <div style={{ fontSize: 10, opacity: 0.7, marginTop: 4, lineHeight: 1.4 }}>{c.blurb}</div>
          </div>
        </div>
      ))}
    </div>
  </section>
);
