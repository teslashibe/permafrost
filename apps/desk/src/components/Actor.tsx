import React, { ReactNode } from 'react';
import { Sprite, CharacterName } from './Sprite';

// Actor positions a sprite at (x, y) in viewport-percent coordinates.
// Animation classes (idle / walking / running-across / swim / perched
// / glowing / alert / halted) drive the CSS keyframes from
// global.css. Optional nameplate is shown on hover; optional speech
// bubble appears for `speech-pop` duration when set.

export interface ActorProps {
  name: CharacterName;
  size?: number;
  /** Viewport percentage X (0-100). */
  x: number;
  /** Viewport percentage Y (0-100). */
  y: number;
  /** Space-separated state classes: idle | walking | running-across |
   *  swim | perched | glowing | alert | halted | coin-fly. */
  state?: string;
  nameplate?: string;
  speech?: string | null;
  zIndex?: number;
  /** Inline CSS variables (e.g. --cx / --cy for coin-fly destination). */
  style?: React.CSSProperties;
  children?: ReactNode;
}

export const Actor: React.FC<ActorProps> = ({
  name, size = 64, x, y, state = '', nameplate, speech, zIndex, style, children,
}) => (
  <div
    className={`actor ${state}`}
    style={{
      left: `${x}%`,
      top: `${y}%`,
      transform: 'translate(-50%, -50%)',
      zIndex,
      ...style,
    }}
  >
    <Sprite name={name} size={size} />
    {nameplate && <div className="nameplate">{nameplate}</div>}
    {speech && <div className="speech">{speech}</div>}
    {children}
  </div>
);
