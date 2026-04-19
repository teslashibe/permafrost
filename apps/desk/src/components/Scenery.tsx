import React, { useMemo } from 'react';

// Scenery layers: ice floes drifting in the foreground, snow flurries
// falling from the sky, an iceberg or two anchored on the horizon.
// Pure decoration -- no data, no interaction.

// Ice floe: a single irregular pale shape drifting slowly across the
// upper-water line. We render two layered floes at different speeds
// for parallax.
export const IceFloes: React.FC = () => (
  <>
    {/* Far floe -- slow, smaller, washed-out */}
    <div className="floe floe-far">
      <svg viewBox="0 0 200 60" preserveAspectRatio="none">
        <ellipse cx="50"  cy="30" rx="46" ry="14" fill="#A8C2DD" opacity="0.5" />
        <ellipse cx="105" cy="33" rx="34" ry="11" fill="#A8C2DD" opacity="0.5" />
        <ellipse cx="160" cy="29" rx="40" ry="13" fill="#A8C2DD" opacity="0.5" />
        <ellipse cx="50"  cy="26" rx="44" ry="10" fill="#ECF6FF" opacity="0.8" />
        <ellipse cx="105" cy="29" rx="32" ry="8"  fill="#ECF6FF" opacity="0.8" />
        <ellipse cx="160" cy="25" rx="38" ry="9"  fill="#ECF6FF" opacity="0.8" />
      </svg>
    </div>
    {/* Near floe -- faster, larger, sharper */}
    <div className="floe floe-near">
      <svg viewBox="0 0 240 60" preserveAspectRatio="none">
        <ellipse cx="60"  cy="33" rx="58" ry="16" fill="#7B9FCB" opacity="0.7" />
        <ellipse cx="170" cy="35" rx="62" ry="17" fill="#7B9FCB" opacity="0.7" />
        <ellipse cx="60"  cy="28" rx="56" ry="12" fill="#ECF6FF" />
        <ellipse cx="170" cy="29" rx="60" ry="13" fill="#ECF6FF" />
      </svg>
    </div>
  </>
);

// Iceberg -- single SVG, anchored on the horizon line on the right
// side. Provides vertical interest and frames Aurora's perch.
export const Iceberg: React.FC<{ x: number; scale?: number }> = ({ x, scale = 1 }) => (
  <div
    className="iceberg"
    style={{ left: `${x}%`, transform: `translateX(-50%) scale(${scale})` }}
  >
    <svg viewBox="0 0 160 220" width="160" height="220">
      {/* Below-water shadow */}
      <polygon
        points="20,150 80,210 140,150 80,220"
        fill="#3D6395" opacity="0.35"
      />
      {/* Iceberg main body */}
      <polygon
        points="80,30 30,150 130,150"
        fill="#E1ECFB"
      />
      {/* Highlight */}
      <polygon
        points="80,30 50,150 80,150"
        fill="#FFFFFF"
      />
      {/* Bottom shading */}
      <polygon
        points="30,150 130,150 110,170 50,170"
        fill="#A8C2DD"
      />
      {/* Tiny snow tuft on top */}
      <ellipse cx="78" cy="28" rx="10" ry="3" fill="#FFFFFF" />
    </svg>
  </div>
);

// SnowFlurry -- 30 falling snowflakes with randomised positions and
// fall durations. Memoised so the random values don't reshuffle on
// every render.
export const SnowFlurry: React.FC = () => {
  const flakes = useMemo(() => {
    const out: Array<{ left: number; size: number; delay: number; dur: number; opacity: number }> = [];
    for (let i = 0; i < 40; i++) {
      out.push({
        left: Math.random() * 100,
        size: 1 + Math.random() * 3,
        delay: Math.random() * 12,
        dur: 8 + Math.random() * 12,
        opacity: 0.4 + Math.random() * 0.5,
      });
    }
    return out;
  }, []);
  return (
    <div className="snow-layer" aria-hidden>
      {flakes.map((f, i) => (
        <span
          key={i}
          className="flake"
          style={{
            left: `${f.left}%`,
            width: f.size,
            height: f.size,
            opacity: f.opacity,
            animationDelay: `-${f.delay}s`,
            animationDuration: `${f.dur}s`,
          }}
        />
      ))}
    </div>
  );
};

// Aurora pulse -- big animated SVG band painted across the upper sky.
// Smoother than the CSS-only gradients and lets us layer multiple
// colours. Combines with the existing ::before drift for depth.
export const AuroraBand: React.FC = () => (
  <svg
    className="aurora-band"
    viewBox="0 0 1200 200"
    preserveAspectRatio="none"
    aria-hidden
  >
    <defs>
      <linearGradient id="aur-g" x1="0%" y1="0%" x2="0%" y2="100%">
        <stop offset="0%"   stopColor="#66E5A8" stopOpacity="0" />
        <stop offset="40%"  stopColor="#66E5A8" stopOpacity="0.45" />
        <stop offset="100%" stopColor="#66E5A8" stopOpacity="0" />
      </linearGradient>
      <linearGradient id="aur-p" x1="0%" y1="0%" x2="0%" y2="100%">
        <stop offset="0%"   stopColor="#B5A6E0" stopOpacity="0" />
        <stop offset="50%"  stopColor="#B5A6E0" stopOpacity="0.45" />
        <stop offset="100%" stopColor="#B5A6E0" stopOpacity="0" />
      </linearGradient>
      <linearGradient id="aur-c" x1="0%" y1="0%" x2="0%" y2="100%">
        <stop offset="0%"   stopColor="#50D2C2" stopOpacity="0" />
        <stop offset="60%"  stopColor="#50D2C2" stopOpacity="0.40" />
        <stop offset="100%" stopColor="#50D2C2" stopOpacity="0" />
      </linearGradient>
    </defs>

    {/* Three slow-rolling bands, offset in phase */}
    <path className="aur-1" d="M 0 100 Q 300 40 600 100 T 1200 100 L 1200 180 L 0 180 Z" fill="url(#aur-g)" />
    <path className="aur-2" d="M 0 110 Q 300 50 600 110 T 1200 110 L 1200 180 L 0 180 Z" fill="url(#aur-p)" />
    <path className="aur-3" d="M 0 90  Q 300 30 600 90  T 1200 90  L 1200 180 L 0 180 Z" fill="url(#aur-c)" />
  </svg>
);
