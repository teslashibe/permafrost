import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  docsSidebar: [
    {
      type: 'category',
      label: 'Introduction',
      collapsed: false,
      items: [
        'introduction/what-is-permafrost',
        'introduction/why-self-custodied',
        'introduction/architecture',
      ],
    },
    {
      type: 'category',
      label: 'Getting Started',
      collapsed: false,
      items: [
        'getting-started/make-demo',
        'getting-started/init-and-doctor',
        'getting-started/local-install',
        'getting-started/configuration',
        'getting-started/prerequisites',
        'getting-started/running-noop',
      ],
    },
    {
      type: 'category',
      label: 'Concepts',
      items: [
        'concepts/primitives',
        'concepts/agent-runtime',
        'concepts/risk-and-killswitch',
        'concepts/reconcile-and-pnl',
        'concepts/inference',
      ],
    },
    {
      type: 'category',
      label: 'Writing a Strategy',
      items: [
        'strategies/sapi',
        'strategies/decision-contract',
        'strategies/services',
        'strategies/reference-strategies',
        'strategies/scaffolding',
        'strategies/private-strategies',
        'strategies/testing',
      ],
    },
    {
      type: 'category',
      label: 'Operations',
      items: [
        'operations/deployment',
        'operations/trading-desk-ui',
        'operations/keystore-and-backups',
        'operations/killswitch-tuning',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      items: [
        'reference/sapi-overview',
        'reference/cli',
      ],
    },
    {
      type: 'category',
      label: 'Brand & Narrative',
      items: [
        'brand/llm-as-agent',
        'brand/cast',
      ],
    },
  ],
};

export default sidebars;
