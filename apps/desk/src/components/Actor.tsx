import React, { ReactNode } from 'react';
import { Sprite, CharacterName } from './Sprite';
import { useDraggable } from '../hooks/useDraggable';

// Actor positions a sprite at (x, y) in viewport-percent coordinates.
// Animation classes (idle / walking / running-across / swim / perched
// / glowing / alert / halted) drive the CSS keyframes from
// global.css. Optional nameplate is shown on hover; optional speech
// bubble appears for `speech-pop` duration when set.
//
// LAYOUT MODEL
// ┌── .actor (positioned at x%, y%, transformed -50%/-50%) ──┐
// │   ┌── .actor-content (THIS gets the walk/swim/etc animation)
// │   │   <img />        (the sprite)
// │   │   children        (e.g. workstation pad under a penguin)
// │   ├──────────────────────────────────────────────────────┤
// │   <nameplate />     (NOT animated; stays attached to .actor)
// │   <speech />         (NOT animated; stays attached to .actor)
// └──────────────────────────────────────────────────────────┘
//
// This split is what makes a penguin's ice-shelf workstation move
// together with the penguin: pass the workstation as `children` and
// it lives inside .actor-content, so the walk keyframe translates
// img + workstation as a single unit.
//
// If dragId is supplied, the actor becomes draggable. Drag updates
// the OUTER .actor position; the inner .actor-content keeps animating.

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
  /**
   * Children render INSIDE .actor-content (alongside the sprite),
   * so they participate in any walk / swim / coin-fly animation
   * applied to the actor. Use for the penguin's workstation pad.
   */
  children?: ReactNode;
}

export const Actor: React.FC<ActorProps> = ({
  name, size = 96, x, y, state = '', nameplate, speech, zIndex, style, dragId, children,
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
      <div className="actor-content">
        <Sprite name={name} size={size} />
        {children}
      </div>
      {nameplate && <div className="nameplate">{nameplate}</div>}
      {speech && <div className="speech">{speech}</div>}
    </div>
  );
};
