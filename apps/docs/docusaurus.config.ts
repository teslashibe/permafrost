import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Permafrost',
  tagline: 'Open-source DeFi trading framework where AI agents deploy capital into your strategies',
  favicon: 'img/favicon.ico',

  future: {
    v4: true,
  },

  // Updated at deploy-time to the project's published URL. Until a custom
  // domain is wired, GitHub Pages serves at https://teslashibe.github.io/permafrost/.
  url: 'https://teslashibe.github.io',
  baseUrl: '/permafrost/',

  organizationName: 'teslashibe',
  projectName: 'permafrost',
  trailingSlash: false,

  // Fail the build on broken cross-references; cheap insurance against doc rot.
  onBrokenLinks: 'throw',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  markdown: {
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
  },
  themes: ['@docusaurus/theme-mermaid'],

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          routeBasePath: '/',
          editUrl: 'https://github.com/teslashibe/permafrost/tree/main/apps/docs/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    colorMode: {
      defaultMode: 'dark',
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Permafrost',
      logo: {
        alt: 'Pole the Polar Bear',
        src: 'img/brand/pole.svg',
        srcDark: 'img/brand/pole.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          to: '/brand/cast',
          position: 'left',
          label: 'The Cast',
        },
        {
          href: 'https://pkg.go.dev/github.com/teslashibe/permafrost',
          label: 'API',
          position: 'right',
        },
        {
          href: 'https://github.com/teslashibe/permafrost',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Introduction', to: '/introduction/what-is-permafrost'},
            {label: 'Getting Started', to: '/getting-started/local-install'},
            {label: 'Writing a Strategy', to: '/strategies/sapi'},
          ],
        },
        {
          title: 'Code',
          items: [
            {label: 'GitHub', href: 'https://github.com/teslashibe/permafrost'},
            {label: 'pkg.go.dev', href: 'https://pkg.go.dev/github.com/teslashibe/permafrost'},
            {label: 'Issues', href: 'https://github.com/teslashibe/permafrost/issues'},
          ],
        },
      ],
      copyright: `MIT licensed. Copyright © ${new Date().getFullYear()} Teslashibe.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['go', 'bash', 'yaml', 'json', 'toml'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
