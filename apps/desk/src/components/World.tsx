import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Agent, DecisionLite } from '../api/client';
import { Actor } from './Actor';
import { Sprite } from './Sprite';
import { AuroraBand, IceFloes, SnowFlurry } from './Scenery';

// World is the animated scene. Each agent gets a workstation slot on
// the ice. Decisions arriving from the API drive transient animations:
//   - Penguin "walks" briefly when a new decision lands.
//   - Penguin shows a speech bubble with the note.
//   - A narwhal swims up if the decision used the LLM.
//   - A coin floats up to the vault if the decision had positive intent.
//   - Aurora's eye blinks if any agent is halted/error.
//
// Skipper the husky runs across the bottom every ~30s (reconcile pass).
// Pole stands at left front-of-stage permanently.
//
// Layout is viewport-percentage based so it adapts to the window size.

const STRATEGIES_WITH_LLM = new Set(['market_maker_basic', 'funding_arb_basic']);

interface Effect {
  id: string;
  kind: 'narwhal' | 'coin' | 'crack';
  x: number;
  y: number;
  expiresAt: number;
  // For coin: where to fly to (also percentage-based)
  toX?: number;
  toY?: number;
}

export interface WorldProps {
  agents: Agent[];
  decisions: DecisionLite[];
  alertActive: boolean;
}

// Workstation slots distributed across the MIDDLE of the ice, in a
// safe band that's clear of every corner HUD:
//   - above the bottom-left decision log    (which extends to ~x=33%)
//   - above the bottom-right cast hud       (which starts at ~x=70%)
//   - below the top-right legend hud        (which ends at ~y=22%)
// Two staggered rows give visual depth without looking like a line-up.
const WORKSTATIONS = [
  { x: 38, y: 56 },   // back row
  { x: 50, y: 53 },   // back center (tallest in perspective)
  { x: 62, y: 56 },
  { x: 44, y: 64 },   // front row (closer to viewer)
  { x: 56, y: 64 },
  { x: 35, y: 60 },
  { x: 65, y: 60 },
  { x: 50, y: 66 },
];

export const World: React.FC<WorldProps> = ({ agents, decisions, alertActive }) => {
  // ── transient effects (narwhals, coins, cracks) ────────────────
  const [effects, setEffects] = useState<Effect[]>([]);
  // Per-agent ephemeral state: speech bubble + walking flag, both
  // expire after a short timeout.
  const [walkers, setWalkers] = useState<Record<string, number>>({});
  const [speeches, setSpeeches] = useState<Record<string, string | undefined>>({});
  // Keep a ref of the most-recent decision id we've processed so we
  // don't re-trigger animations on every poll.
  const seenIds = useRef<Set<string>>(new Set());

  // Skipper used to be triggered via React-key remount which caused
  // a brief "teleport" to the start of his run path. He now uses a
  // single infinite CSS animation so he always glides rightward,
  // pauses off-screen, and re-emerges from the left -- never moves
  // backwards. No state needed here.

  // React to new decisions. Each new decision triggers a small set of
  // animations on the corresponding agent's actor.
  useEffect(() => {
    if (!decisions.length) return;
    const now = Date.now();
    const added: Effect[] = [];
    const speechUpdates: Record<string, string> = {};
    const walkUpdates: Record<string, number> = {};

    for (const d of decisions) {
      if (seenIds.current.has(d.id)) continue;
      seenIds.current.add(d.id);

      // Find the agent's workstation.
      const agentIdx = agents.findIndex(a => a.id === d.agent_id);
      if (agentIdx < 0) continue;
      const slot = WORKSTATIONS[agentIdx % WORKSTATIONS.length];

      walkUpdates[d.agent_id] = now + 4_000;
      if (d.notes) speechUpdates[d.agent_id] = d.notes;

      // LLM consult? Send a narwhal swimming to this agent.
      if (d.llm_used) {
        added.push({
          id: `n-${d.id}`,
          kind: 'narwhal',
          x: slot.x,
          y: slot.y - 8,
          expiresAt: now + 5_000,
        });
      }
      // Decision had any orders or swaps? Drop a coin that flies up
      // toward the vault HUD (top-left, ~12% x, ~12% y).
      if (d.num_orders > 0 || d.num_swaps > 0) {
        added.push({
          id: `c-${d.id}`,
          kind: 'coin',
          x: slot.x,
          y: slot.y - 12,
          toX: 12 - slot.x,   // delta in viewport-% from spawn -> vault
          toY: 12 - (slot.y - 12),
          expiresAt: now + 2_500,
        });
      }
    }

    if (added.length) setEffects(eff => [...eff, ...added]);
    if (Object.keys(speechUpdates).length) setSpeeches(s => ({ ...s, ...speechUpdates }));
    if (Object.keys(walkUpdates).length)   setWalkers(w => ({ ...w, ...walkUpdates }));
  }, [decisions, agents]);

  // Sweep expired effects + speech bubbles.
  useEffect(() => {
    const t = setInterval(() => {
      const now = Date.now();
      setEffects(eff => eff.filter(e => e.expiresAt > now));
      setWalkers(w => {
        const out: Record<string, number> = {};
        for (const [k, v] of Object.entries(w)) if (v > now) out[k] = v;
        return out;
      });
      setSpeeches(s => {
        // Keep the latest speech for each agent for ~3s after walker
        // expires. Cheap heuristic: drop any speech for an agent whose
        // walker has expired.
        const out: Record<string, string | undefined> = {};
        for (const k of Object.keys(s)) if (walkers[k]) out[k] = s[k];
        return out;
      });
    }, 1_000);
    return () => clearInterval(t);
  }, [walkers]);

  // Build the per-agent actor list.
  const agentActors = useMemo(() => agents.slice(0, WORKSTATIONS.length).map((a, i) => {
    const slot = WORKSTATIONS[i];
    const isHalted = a.status === 'halted' || a.status === 'error';
    const isWalking = walkers[a.id] && !isHalted;
    const usesLLM = STRATEGIES_WITH_LLM.has(a.strategy);
    const state = isHalted ? 'halted' : (isWalking ? 'walking' : 'idle');
    return (
      <React.Fragment key={a.id}>
        {/* Workstation pad under the penguin's feet */}
        <div
          className="workstation"
          style={{ left: `${slot.x}%`, top: `calc(${slot.y}% + 38px)` }}
        >
          <div className="top" />
          {/* small "alive" beacon when the agent is running */}
          {!isHalted && <div className="glow" />}
        </div>

        <Actor
          name="penguin"
          x={slot.x}
          y={slot.y}
          size={84}
          state={state}
          nameplate={`${a.name || a.id.slice(0, 12)} - ${a.strategy}`}
          speech={speeches[a.id]}
          zIndex={20}
          dragId={`actor:penguin:${a.id}`}
        />
        {/* Permanent narwhal companion for LLM-using strategies,
            floating in the sky above the workstation. Draggable
            independently -- if the user drags the penguin, the
            narwhal stays where the user puts it (and vice versa). */}
        {usesLLM && (
          <Actor
            name="narwhal"
            x={slot.x}
            y={slot.y - 24}
            size={56}
            state="perched"
            nameplate={`${a.name || a.id.slice(0, 12)}'s LLM advisor`}
            zIndex={19}
            dragId={`actor:narwhal:${a.id}`}
          />
        )}
      </React.Fragment>
    );
  }), [agents, walkers, speeches]);

  return (
    <div className="world">
      {/* === scenery (sky + horizon + drifters) === */}
      <AuroraBand />
      <IceFloes />
      <SnowFlurry />

      {/* === permanent characters === */}

      {/* Pole the Polar Bear sits stage-left, watches the camp.
          Draggable. */}
      <Actor
        name="pole"
        x={12}
        y={58}
        size={150}
        state="idle"
        nameplate="Pole the Polar Bear"
        zIndex={25}
        dragId="actor:pole"
      />

      {/* Aurora the snowy owl floats high above the right side of
          the ice. Draggable. */}
      <Actor
        name="owl"
        x={87}
        y={42}
        size={88}
        state={`perched ${alertActive ? 'alert' : ''}`}
        nameplate={alertActive ? 'Aurora is ALERT' : 'Aurora is watching'}
        zIndex={26}
        dragId="actor:owl"
      />

      {/* Kelp the walrus on a small ice patch in front of Pole.
          Draggable. */}
      <Actor
        name="walrus"
        x={23}
        y={66}
        size={104}
        state="idle"
        nameplate="Kelp - swap router"
        zIndex={22}
        dragId="actor:walrus"
      />

      {/* Skipper trots rightward forever via a single infinite CSS
          animation -- runs across the ice, exits right, pauses,
          re-enters from the left. Never moves backwards. */}
      <Actor
        name="husky"
        x={-5}
        y={70}
        size={108}
        state="running-across"
        nameplate="Skipper"
        zIndex={28}
      />

      {/* === per-agent penguin traders === */}
      {agentActors}

      {/* === transient effects === */}
      {effects.map(e => {
        if (e.kind === 'narwhal') {
          return (
            <Actor
              key={e.id}
              name="narwhal"
              x={e.x}
              y={e.y}
              size={48}
              state="swim glowing"
              zIndex={30}
            />
          );
        }
        if (e.kind === 'coin') {
          return (
            <Actor
              key={e.id}
              name="coin"
              x={e.x}
              y={e.y}
              size={28}
              state="coin-fly"
              zIndex={35}
              style={{
                ['--cx' as string]: `${(e.toX ?? 0) * 12}px`,
                ['--cy' as string]: `${(e.toY ?? -25) * 12}px`,
              } as React.CSSProperties}
            />
          );
        }
        return null;
      })}

      {/* Tusk lurks at the back-right of the ice IF a private strategy
          is registered. Maintainer easter egg -- visible only on
          the maintainer's local build. Draggable. */}
      {agents.some(a => a.strategy === 'funding_arb_basic') && (
        <Actor
          name="mammoth"
          x={78}
          y={51}
          size={80}
          state="idle"
          nameplate="Tusk - private"
          zIndex={15}
          dragId="actor:tusk"
        />
      )}

      {/* Frostbite the Whale surfaces from the bottom-front of the
          ice when an agent halts. Big, ominous, hard to miss. */}
      {alertActive && (
        <Actor
          name="whale"
          x={50}
          y={102}
          size={160}
          state=""
          nameplate="Frostbite has surfaced"
          zIndex={40}
        />
      )}

      {/* A small "+legend" hint for the non-obvious sprites */}
      <SpriteHint />
    </div>
  );
};

// SpriteHint shows a 1-line legend pinned to the bottom-centre,
// rotating between characters every 6s. Onboarding without taking
// up real estate.
const SpriteHint: React.FC = () => {
  const HINTS = [
    { name: 'pole'    as const, text: 'Pole the Polar Bear watches the camp.' },
    { name: 'owl'     as const, text: 'Aurora the owl monitors risk.' },
    { name: 'penguin' as const, text: 'Penguin traders execute strategy decisions.' },
    { name: 'narwhal' as const, text: 'Narwhals swim up when an LLM consult fires.' },
    { name: 'walrus'  as const, text: 'Kelp the walrus routes swaps between chains.' },
    { name: 'husky'   as const, text: 'Skipper trots between camps reconciling state.' },
  ];
  const [i, setI] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setI(n => (n + 1) % HINTS.length), 6_000);
    return () => clearInterval(t);
  }, [HINTS.length]);
  const h = HINTS[i];
  return (
    <div style={{
      // Pinned ABOVE the disconnect-footer (which sits at bottom: 12)
      // so the two never overlap when the daemon is unreachable.
      position: 'fixed', bottom: 56, left: '50%', transform: 'translateX(-50%)',
      zIndex: 40, fontSize: 11, opacity: 0.85,
      display: 'flex', alignItems: 'center', gap: 8,
      background: 'rgba(10,27,54,0.78)', padding: '6px 14px', borderRadius: 14,
      border: '1px solid var(--ice-edge)',
      backdropFilter: 'blur(4px)',
    }}>
      <Sprite name={h.name} size={24} />
      <span>{h.text}</span>
    </div>
  );
};
