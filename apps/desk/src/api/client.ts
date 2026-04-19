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

// Demo / disconnected mode — what the UI shows when the daemon isn't
// reachable. Lets operators preview the experience without `make up`.
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
      updated_at: new Date().toISOString(),
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
      updated_at: new Date().toISOString(),
    },
  ],
  recent_decisions: [
    {
      id: 'd-1',
      agent_id: 'ag-pip-01',
      ts: new Date(Date.now() - 5_000).toISOString(),
      confidence: 0,
      notes: 'noop',
      num_orders: 0,
      num_swaps: 0,
      llm_used: false,
    },
    {
      id: 'd-2',
      agent_id: 'ag-boulder-02',
      ts: new Date(Date.now() - 12_000).toISOString(),
      confidence: 1,
      notes: 'dca buy: 50 USDC → SOL on solana',
      num_orders: 0,
      num_swaps: 1,
      llm_used: false,
    },
  ],
};
