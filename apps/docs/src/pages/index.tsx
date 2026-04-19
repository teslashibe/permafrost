import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';

import styles from './index.module.css';

function HomepageHeader() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={clsx('hero', styles.heroBanner)}>
      <div className="container">
        <Heading as="h1" className="hero__title">
          {siteConfig.title}
        </Heading>
        <p className="hero__subtitle">{siteConfig.tagline}</p>
        <div className={styles.buttons}>
          <Link
            className="button button--primary button--lg"
            to="/introduction/what-is-permafrost">
            Read the docs
          </Link>
          <Link
            className="button button--secondary button--lg"
            style={{marginLeft: '0.75rem'}}
            href="https://github.com/teslashibe/permafrost">
            View on GitHub
          </Link>
        </div>
      </div>
    </header>
  );
}

const PROPS = [
  {
    title: 'Open framework, your strategies',
    body: (
      <>
        Drop a folder under <code>strategies/</code>, register in <code>init()</code>,
        add one blank-import line to enable it. Same shape as Hummingbot, in Go.
      </>
    ),
  },
  {
    title: 'Self-custodied by design',
    body: (
      <>
        Spot positions live in your own wallet. Perp orders are signed by your local
        keystore. No exchange custody, no rehypothecation.
      </>
    ),
  },
  {
    title: 'AI augments, never invents',
    body: (
      <>
        Strategies are deterministic Go. The LLM can veto entries, score candidates,
        and tune thresholds — but never produces orders directly.
      </>
    ),
  },
  {
    title: 'Auditable to the prompt',
    body: (
      <>
        Every decision is persisted with the exact prompt, model response, and resulting
        on-chain action. Joinable in TimescaleDB.
      </>
    ),
  },
];

function Props(): ReactNode {
  return (
    <section className={styles.props}>
      <div className="container">
        <div className="row">
          {PROPS.map((p) => (
            <div key={p.title} className="col col--6">
              <div className={styles.prop}>
                <Heading as="h3">{p.title}</Heading>
                <p>{p.body}</p>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  return (
    <Layout
      title="Permafrost"
      description="Open-source DeFi trading framework where AI agents deploy capital into your strategies.">
      <HomepageHeader />
      <main>
        <Props />
      </main>
    </Layout>
  );
}
