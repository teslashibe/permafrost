import React from 'react';

// Each character is a static .svg under src/characters/. Vite's
// ?react import would compile the SVG into a component, but we
// avoid that build dep — we just use <img> with the raw asset URL.
// The CSS classes (.glow, .alert .eye, .shimmer) on the SVG markup
// do all the animation work without React having to re-render.

import poleUrl    from '../characters/pole.svg';
import penguinUrl from '../characters/penguin.svg';
import narwhalUrl from '../characters/narwhal.svg';
import owlUrl     from '../characters/owl.svg';
import huskyUrl   from '../characters/husky.svg';
import walrusUrl  from '../characters/walrus.svg';
import whaleUrl   from '../characters/whale.svg';
import mammothUrl from '../characters/mammoth.svg';
import coinUrl    from '../characters/coin.svg';

export type CharacterName =
  | 'pole'
  | 'penguin'
  | 'narwhal'
  | 'owl'
  | 'husky'
  | 'walrus'
  | 'whale'
  | 'mammoth'
  | 'coin';

const SOURCES: Record<CharacterName, string> = {
  pole:    poleUrl,
  penguin: penguinUrl,
  narwhal: narwhalUrl,
  owl:     owlUrl,
  husky:   huskyUrl,
  walrus:  walrusUrl,
  whale:   whaleUrl,
  mammoth: mammothUrl,
  coin:    coinUrl,
};

const LABELS: Record<CharacterName, string> = {
  pole:    'Pole the Polar Bear',
  penguin: 'Penguin trader',
  narwhal: 'Narwhal LLM advisor',
  owl:     'Aurora the snowy owl (risk monitor)',
  husky:   'Skipper (reconciliation runner)',
  walrus:  'Kelp the walrus (swap router)',
  whale:   'Frostbite the Whale (killswitch)',
  mammoth: 'Tusk the mammoth (private strategy)',
  coin:    'Coin (PnL)',
};

export interface SpriteProps {
  name: CharacterName;
  size?: number; // px; defaults to 64
  className?: string;
  title?: string;
}

export const Sprite: React.FC<SpriteProps> = ({ name, size = 64, className, title }) => (
  <img
    src={SOURCES[name]}
    width={size}
    height={size}
    alt={LABELS[name]}
    title={title ?? LABELS[name]}
    className={className}
    style={{
      imageRendering: 'pixelated',
      display: 'inline-block',
      verticalAlign: 'middle',
    }}
  />
);
