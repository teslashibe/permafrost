// Permafrost daemon API client.
//
// V1 surface — what we need for the Trading Desk's read-only views.
// Mutations (start/stop/set-mode) come in v2 once the WebSocket /
// SSE channel is in.
//
// All requests go through the Vite dev-proxy when running with
// `npm run dev` (proxy /v1 -> http://127.0.0.1:8080 in vite.config.ts).
// In production (when the daemon embeds the built UI under /ui), the
// same paths work without a proxy.

export type AgentMode = 'paper' | 'live';
export type AgentStatus = 'idle' | 'running' | 'halted' | 'error';

export interface Agent {
  id: string;
  name: string;
  strategy: string;
  perp_venue: string;
  spot_venue: string;
  mode: AgentMode;
  status: AgentStatus;
  alloc_usd: string;
  network: string;
  tick_secs: number;
  updated_at: string;
}

export interface DecisionLite {
  id: string;
  agent_id: string;
  ts: string;
  confidence: number;
  notes: string;
  num_orders: number;
  num_swaps: number;
  llm_used: boolean;
}

export interface DemoData {
  agents: Agent[];
  recent_decisions: DecisionLite[];
}

export class APIClient {
  constructor(private base = '') {}

  async health(): Promise<{ ok: boolean; version?: string }> {
    return this.get('/v1/health');
  }

  async listAgents(): Promise<Agent[]> {
    return this.get('/v1/agents');
  }

  async recentDecisions(agentID: string, limit = 20): Promise<DecisionLite[]> {
    const q = new URLSearchParams({ limit: String(limit) });
    return this.get(`/v1/agents/${encodeURIComponent(agentID)}/decisions?${q.toString()}`);
  }

  private async get<T>(path: string): Promise<T> {
    const res = await fetch(`${this.base}${path}`);
    if (!res.ok) {
      throw new APIError(res.status, await res.text());
    }
    return res.json();
  }
}

export class APIError extends Error {
  constructor(public status: number, public body: string) {
    super(`api ${status}: ${body}`);
    this.name = 'APIError';
  }
}

// Demo / disconnected mode -- what the UI shows when the daemon
// isn't reachable. Provides a rich scene: 4 agents covering the
// shipped public strategies (noop, dca_buy, market_maker_basic) plus
// one private-strategy slot to demo the Tusk easter egg, alongside a
// steady stream of decisions so the world has something to animate.
//
// `nextDemoBatch()` returns a fresh batch every call, simulating
// activity over time. App.tsx uses static `demoData` for the agent
// list, but cycles through `nextDemoBatch()` for decisions so the
// scene actually feels alive while disconnected.

const ts = (offsetMs: number) => new Date(Date.now() - offsetMs).toISOString();

export const demoData: DemoData = {
  agents: [
    {
      id: 'ag-pip-01',
      name: 'Pip',
      strategy: 'noop',
      perp_venue: 'hyperliquid',
      spot_venue: '',
      mode: 'paper',
      status: 'running',
      alloc_usd: '1000.00',
      network: 'mainnet',
      tick_secs: 5,
      updated_at: ts(0),
    },
    {
      id: 'ag-boulder-02',
      name: 'Boulder',
      strategy: 'dca_buy',
      perp_venue: 'hyperliquid',
      spot_venue: 'jupiter',
      mode: 'paper',
      status: 'running',
      alloc_usd: '500.00',
      network: 'mainnet',
      tick_secs: 60,
      updated_at: ts(0),
    },
    {
      id: 'ag-mira-03',
      name: 'Mira',
      strategy: 'market_maker_basic',
      perp_venue: 'hyperliquid',
      spot_venue: '',
      mode: 'paper',
      status: 'running',
      alloc_usd: '750.00',
      network: 'mainnet',
      tick_secs: 30,
      updated_at: ts(0),
    },
    {
      id: 'ag-arlo-04',
      // Placeholder name representing an operator's privately-registered
      // strategy (Hummingbot-style local extension under
      // strategies/private/, blank-imported from the gitignored
      // cmd/permafrost(d)/strategies_local.go). Anything not in
      // PUBLIC_STRATEGIES (in World.tsx) triggers the Tusk easter egg.
      name: 'Arlo',
      strategy: 'your_private_strategy',
      perp_venue: 'hyperliquid',
      spot_venue: 'jupiter',
      mode: 'paper',
      status: 'running',
      alloc_usd: '1500.00',
      network: 'mainnet',
      tick_secs: 30,
      updated_at: ts(0),
    },
  ],
  recent_decisions: [
    {
      id: 'd-pip-1',
      agent_id: 'ag-pip-01',
      ts: ts(3_000),
      confidence: 0,
      notes: 'noop tick',
      num_orders: 0,
      num_swaps: 0,
      llm_used: false,
    },
    {
      id: 'd-boulder-1',
      agent_id: 'ag-boulder-02',
      ts: ts(8_000),
      confidence: 1,
      notes: 'dca buy: 50 USDC -> SOL on solana',
      num_orders: 0,
      num_swaps: 1,
      llm_used: false,
    },
    {
      id: 'd-mira-1',
      agent_id: 'ag-mira-03',
      ts: ts(12_000),
      confidence: 0.7,
      notes: 'quote WIF: bid=0.997 ask=1.003 mid=1.000 spread=25bps',
      num_orders: 2,
      num_swaps: 0,
      llm_used: true,
    },
    {
      id: 'd-arlo-1',
      agent_id: 'ag-arlo-04',
      ts: ts(18_000),
      confidence: 0.85,
      notes: 'private strategy: WIF basis=42bps (sample decision)',
      num_orders: 1,
      num_swaps: 1,
      llm_used: false,
    },
    {
      id: 'd-mira-2',
      agent_id: 'ag-mira-03',
      ts: ts(45_000),
      confidence: 0.0,
      notes: 'vetoed: incoming funding flip; skip refresh',
      num_orders: 0,
      num_swaps: 0,
      llm_used: true,
    },
  ],
};

// nextDemoBatch advances the demo scene -- produces new decisions
// "happening" right now so the world's transient effects (narwhal
// swims, coin flies, speech bubbles) actually trigger while you watch.
// Call from a setInterval in App.tsx.
let demoCounter = 0;
const DEMO_TEMPLATES: Array<Omit<DecisionLite, 'id' | 'ts'>> = [
  { agent_id: 'ag-pip-01',     confidence: 0.0, notes: 'noop tick',
    num_orders: 0, num_swaps: 0, llm_used: false },
  { agent_id: 'ag-boulder-02', confidence: 1.0, notes: 'dca buy: 50 USDC -> SOL on solana',
    num_orders: 0, num_swaps: 1, llm_used: false },
  { agent_id: 'ag-mira-03',    confidence: 0.7, notes: 'quote WIF: bid=0.998 ask=1.002 mid=1.000',
    num_orders: 2, num_swaps: 0, llm_used: true },
  { agent_id: 'ag-arlo-04',    confidence: 0.85, notes: 'private strategy: enter basis BONK',
    num_orders: 1, num_swaps: 1, llm_used: true },
  { agent_id: 'ag-mira-03',    confidence: 0.0, notes: 'vetoed: high vol, skip',
    num_orders: 0, num_swaps: 0, llm_used: true },
  { agent_id: 'ag-pip-01',     confidence: 0.0, notes: 'noop tick',
    num_orders: 0, num_swaps: 0, llm_used: false },
  { agent_id: 'ag-arlo-04',    confidence: 0.9, notes: 'private strategy: close basis BONK +12bps',
    num_orders: 1, num_swaps: 1, llm_used: false },
];
export function nextDemoBatch(prev: DecisionLite[]): DecisionLite[] {
  const tpl = DEMO_TEMPLATES[demoCounter % DEMO_TEMPLATES.length];
  demoCounter++;
  const fresh: DecisionLite = {
    ...tpl,
    id: `d-demo-${demoCounter}`,
    ts: new Date().toISOString(),
  };
  // Keep the most recent 30, newest first.
  return [fresh, ...prev].slice(0, 30);
}
