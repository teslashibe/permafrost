import React, { ReactNode } from 'react';
import { Sprite, CharacterName } from './Sprite';
import { useDraggable } from '../hooks/useDraggable';

// Actor positions a sprite at (x, y) in viewport-percent coordinates.
// Animation classes (idle / walking / running-across / swim / perched
// / glowing / alert / halted) drive the CSS keyframes from
// global.css. Optional nameplate is shown on hover; optional speech
// bubble appears for `speech-pop` duration when set.
//
// If dragId is supplied, the actor becomes draggable by the user --
// click-and-drag anywhere on the sprite moves it, and the position
// is persisted in localStorage under the supplied id (use the
// "actor:<name>" namespace to avoid collisions with HUD ids).

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
  /**
   * If provided, the actor is draggable by the user. Position is
   * stored in localStorage under `permafrost-desk:<dragId>`. Use a
   * stable id like `actor:pole` or `actor:penguin:<agent-id>`.
   */
  dragId?: string;
  children?: ReactNode;
}

export const Actor: React.FC<ActorProps> = ({
  name, size = 64, x, y, state = '', nameplate, speech, zIndex, style, dragId, children,
}) => {
  // Always call the hook (rules of hooks) but pass an empty id if
  // not draggable -- the hook short-circuits in that case and
  // returns inert handlers + an empty style override.
  const drag = useDraggable(dragId ?? '');
  const isDraggable = !!dragId;

  return (
    <div
      ref={isDraggable ? drag.ref : undefined}
      className={`actor ${state}${isDraggable ? ' draggable' : ''}`}
      onPointerDown={isDraggable ? drag.handleProps.onPointerDown : undefined}
      style={{
        left: `${x}%`,
        top: `${y}%`,
        transform: 'translate(-50%, -50%)',
        zIndex,
        ...(isDraggable ? drag.handleProps.style : null),
        ...style,
        // Spread drag.style LAST so a user-set position overrides
        // the base x/y placement (and the centering transform).
        ...(isDraggable ? drag.style : null),
      }}
    >
      <Sprite name={name} size={size} />
      {nameplate && <div className="nameplate">{nameplate}</div>}
      {speech && <div className="speech">{speech}</div>}
      {children}
    </div>
  );
};
