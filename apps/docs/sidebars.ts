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
      items: [
        'getting-started/prerequisites',
        'getting-started/local-install',
        'getting-started/configuration',
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
        'strategies/private-strategies',
        'strategies/testing',
      ],
    },
    {
      type: 'category',
      label: 'Operations',
      items: [
        'operations/deployment',
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
