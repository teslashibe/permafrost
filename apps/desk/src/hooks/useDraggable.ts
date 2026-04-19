import { useEffect, useRef, useState } from 'react';

// useDraggable -- makes any element draggable by its handle and
// persists the user-set position in localStorage under a stable id.
//
// Two-state model so we don't have to compute initial pixel positions
// for every HUD up front:
//   - `pos === null` (default)         -> caller's CSS class controls
//                                         positioning (e.g. .hud.vault
//                                         { top: 80; left: 24 }).
//   - `pos !== null` (user dragged)    -> inline style overrides CSS
//                                         with `top: <px>; left: <px>;
//                                         right: auto; bottom: auto`.
//                                         Also persisted.
//
// On reload, anything in localStorage is rehydrated synchronously so
// the HUD opens at the user's last position without a visible flicker.
//
// Drag bounds are constrained to the viewport so a HUD can't be lost
// off-screen.

interface Pos { top: number; left: number; }

export interface DraggableHandle {
  ref: React.RefObject<HTMLDivElement>;
  /**
   * Inline style to spread on the draggable element. Empty object when
   * no user position has been set, so the caller's CSS class wins.
   */
  style: React.CSSProperties;
  /**
   * Spread these props on the drag handle (typically the title/header)
   * so a click-and-drag on the header moves the whole element.
   */
  handleProps: {
    onPointerDown: (e: React.PointerEvent) => void;
    style: React.CSSProperties;
  };
}

export function useDraggable(id: string): DraggableHandle {
  // Caller passes a fully-qualified id including its namespace
  // (e.g. "hud:vault", "actor:pole", "actor:penguin:ag-pip-01") so
  // sprites and HUDs can share this hook without colliding.
  const storageKey = id ? `permafrost-desk:${id}` : '';
  const ref = useRef<HTMLDivElement>(null);

  const [pos, setPos] = useState<Pos | null>(() => {
    if (!id || typeof window === 'undefined') return null;
    try {
      const raw = localStorage.getItem(storageKey);
      if (!raw) return null;
      const parsed = JSON.parse(raw);
      if (typeof parsed?.top === 'number' && typeof parsed?.left === 'number') {
        return { top: parsed.top, left: parsed.left };
      }
    } catch {
      /* ignore -- localStorage might be disabled or corrupt */
    }
    return null;
  });

  // Mutable drag state, cleared between drags.
  const dragRef = useRef<{
    x0: number; y0: number; left0: number; top0: number; pointerId: number;
  } | null>(null);

  // We use Pointer Events (not mouse events) so the same handler
  // works for mouse, touch, and pen. setPointerCapture pins the
  // pointer to the handle for the whole drag, so even fast moves
  // outside the original element keep firing pointermove on it.
  const onPointerDown = (e: React.PointerEvent) => {
    if (!ref.current) return;
    if (e.button !== 0 && e.pointerType === 'mouse') return;
    e.preventDefault();

    const rect = ref.current.getBoundingClientRect();
    dragRef.current = {
      x0: e.clientX,
      y0: e.clientY,
      left0: rect.left,
      top0: rect.top,
      pointerId: e.pointerId,
    };

    // Capture the pointer on the handle element. After this, all
    // subsequent pointermove/pointerup with the same pointerId fire
    // on the handle, regardless of the cursor's actual position.
    const handle = e.currentTarget as Element;
    try { handle.setPointerCapture(e.pointerId); } catch { /* noop */ }

    const onMove = (ev: PointerEvent) => {
      if (!dragRef.current || !ref.current) return;
      if (ev.pointerId !== dragRef.current.pointerId) return;
      const dx = ev.clientX - dragRef.current.x0;
      const dy = ev.clientY - dragRef.current.y0;
      const w = ref.current.offsetWidth;
      const h = ref.current.offsetHeight;
      const left = clamp(dragRef.current.left0 + dx, 0, window.innerWidth - w);
      const top = clamp(dragRef.current.top0 + dy, 0, window.innerHeight - h);
      setPos({ top, left });
    };

    const onMoveListener = onMove as EventListener;
    const onUpListener = ((ev: Event) => {
      const pe = ev as PointerEvent;
      if (dragRef.current && pe.pointerId !== dragRef.current.pointerId) return;
      handle.removeEventListener('pointermove', onMoveListener);
      handle.removeEventListener('pointerup', onUpListener);
      handle.removeEventListener('pointercancel', onUpListener);
      try { handle.releasePointerCapture(pe.pointerId); } catch { /* noop */ }
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      dragRef.current = null;
    }) as EventListener;

    handle.addEventListener('pointermove', onMoveListener);
    handle.addEventListener('pointerup', onUpListener);
    handle.addEventListener('pointercancel', onUpListener);
    document.body.style.cursor = 'grabbing';
    // Block accidental text selection while dragging.
    document.body.style.userSelect = 'none';
  };

  // Persist whenever the user-set position changes.
  useEffect(() => {
    if (!id || pos === null) return;
    try {
      localStorage.setItem(storageKey, JSON.stringify(pos));
    } catch {
      /* ignore */
    }
  }, [pos, storageKey, id]);

  // When pos is set, override the host element's transform too.
  // Actors use `transform: translate(-50%, -50%)` to centre on their
  // (x%, y%) position; without overriding that, a dragged actor would
  // jump by half its size on the first move. Setting transform: none
  // makes the user-set top/left the literal top-left of the element.
  const style: React.CSSProperties = pos
    ? { top: pos.top, left: pos.left, right: 'auto', bottom: 'auto', transform: 'none' }
    : {};

  return {
    ref,
    style,
    handleProps: {
      onPointerDown,
      style: { cursor: 'grab', userSelect: 'none', touchAction: 'none' },
    },
  };
}

function clamp(v: number, lo: number, hi: number) {
  return Math.max(lo, Math.min(hi, v));
}
