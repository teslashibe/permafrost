import type {ReactNode} from 'react';
import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';
import Layout from '@theme/Layout';

import styles from './index.module.css';

// Docs landing page styled to match the Trading Desk UI:
// arctic night sky with aurora + stars + ice horizon, Pole the
// Polar Bear avatar in the hero, terminal-style quickstart card,
// feature-prop cards anchored to sprites, and the cast showcase.

interface CastEntry {
  sprite: string;       // filename under /img/brand/
  name: string;
  role: string;
  href: string;         // docs page describing this character/concept
}

const CAST: CastEntry[] = [
  { sprite: 'pole.svg',    name: 'Pole the Polar Bear', role: 'Camp Director', href: '/brand/cast' },
  { sprite: 'penguin.svg', name: 'Penguin trader',      role: 'Strategy agent', href: '/concepts/agent-runtime' },
  { sprite: 'narwhal.svg', name: 'Narwhal advisor',     role: 'LLM consult',     href: '/concepts/inference' },
  { sprite: 'owl.svg',     name: 'Aurora the owl',      role: 'Risk monitor',    href: '/concepts/risk-and-killswitch' },
  { sprite: 'husky.svg',   name: 'Skipper',             role: 'Reconciler',      href: '/concepts/reconcile-and-pnl' },
  { sprite: 'walrus.svg',  name: 'Kelp the walrus',     role: 'Swap router',     href: '/concepts/primitives' },
  { sprite: 'whale.svg',   name: 'Frostbite',           role: 'Killswitch',      href: '/operations/killswitch-tuning' },
  { sprite: 'mammoth.svg', name: 'Tusk',                role: 'Private strategy', href: '/strategies/private-strategies' },
];

interface PropEntry {
  sprite: string;
  title: string;
  body: ReactNode;
}

const PROPS: PropEntry[] = [
  {
    sprite: 'penguin.svg',
    title: 'Open framework, your strategies',
    body: (
      <>
        Drop a folder under <code>strategies/</code>, register in <code>init()</code>,
        add one blank import to enable it. Same shape as Hummingbot, in Go.
      </>
    ),
  },
  {
    sprite: 'pole.svg',
    title: 'Self-custodied by design',
    body: (
      <>
        Spot positions live in your own wallet. Perp orders are signed by your local
        keystore. No exchange custody, no rehypothecation.
      </>
    ),
  },
  {
    sprite: 'narwhal.svg',
    title: 'AI augments, never invents',
    body: (
      <>
        Strategies are deterministic Go. The LLM can veto entries, score candidates,
        and tune thresholds -- but never produces orders directly.
      </>
    ),
  },
  {
    sprite: 'owl.svg',
    title: 'Auditable to the prompt',
    body: (
      <>
        Every decision is persisted with the exact prompt, model response, and
        resulting on-chain action. Joinable in TimescaleDB.
      </>
    ),
  },
  {
    sprite: 'whale.svg',
    title: 'Real killswitch',
    body: (
      <>
        <code>agent stop --all</code> cancels every open order, flattens shorts via
        reduce-only market orders, and (opt-in) liquidates spot legs back to USDC.
      </>
    ),
  },
  {
    sprite: 'husky.svg',
    title: 'Reconciliation built in',
    body: (
      <>
        Framework state matches venue truth after every tick. Drift is detected and
        surfaces in the decision log; the killswitch can engage on persistent drift.
      </>
    ),
  },
];

function HeroBanner() {
  const poleUrl = useBaseUrl('/img/brand/pole.svg');
  return (
    <header className={styles.hero}>
      <div className={styles.heroContent}>
        <div className={styles.heroBadge}>
          <span className={styles.dot} />
          v0.1.0 &middot; the camp is open
        </div>
        <h1 className={styles.heroTitle}>
          Permafrost. Your AI trading desk,<br />
          locked in the ice.
        </h1>
        <p className={styles.heroSubtitle}>
          A Go framework for self-custodied algorithmic trading. Write deterministic
          strategies, optionally augment them with an LLM, and run them locally on
          a real arctic-themed operator dashboard.
        </p>
        <div className={styles.heroButtons}>
          <Link
            className={`button button--lg ${styles.btnPrimary}`}
            to="/getting-started/make-demo">
            Run the demo &rarr;
          </Link>
          <Link
            className={`button button--lg ${styles.btnSecondary}`}
            to="/introduction/what-is-permafrost">
            Read the docs
          </Link>
          <Link
            className={`button button--lg ${styles.btnSecondary}`}
            href="https://github.com/teslashibe/permafrost">
            {'GitHub \u2197'}
          </Link>
        </div>
        <div className={styles.heroPole}>
          <img src={poleUrl} alt="Pole the Polar Bear, Camp Director" />
          <div className={styles.heroPoleLabel}>Camp Director</div>
        </div>
      </div>
    </header>
  );
}

function QuickStart() {
  return (
    <section className={styles.quickStart}>
      <div className={styles.quickStartInner}>
        <div className={styles.quickStartLabel}>quick start</div>
        <h2 className={styles.quickStartHeading}>One command, from clone to decisions</h2>
        <div className={styles.terminal}>
          <div className={styles.terminalChrome}>
            <span className={styles.cdot} />
            <span className={styles.cdot} />
            <span className={styles.cdot} />
            <span className={styles.cTitle}>~/permafrost</span>
          </div>
          <div className={styles.terminalBody}>
            <span className={styles.comment}># Clone, build, run. Idempotent: re-run any time.</span>{'\n'}
            <span className={styles.prompt}>$</span> git clone https://github.com/teslashibe/permafrost.git{'\n'}
            <span className={styles.prompt}>$</span> cd permafrost{'\n'}
            <span className={styles.prompt}>$</span> make demo{'\n'}
            {'\n'}
            <span className={styles.comment}># Brings up Postgres + permafrostd in Docker, runs the init wizard,</span>{'\n'}
            <span className={styles.comment}># spins up a paper-mode noop agent, and tails its decisions.</span>{'\n'}
            {'\n'}
            <span className={styles.heart}>&#10084;</span> &nbsp;Pip is on the ice.{'\n'}
            <span className={styles.comment}>[tick 1] confidence=0.00 swaps=0 orders=0 notes="noop"</span>{'\n'}
            <span className={styles.comment}>[tick 2] confidence=0.00 swaps=0 orders=0 notes="noop"</span>
          </div>
        </div>
      </div>
    </section>
  );
}

function Props() {
  return (
    <section className={styles.props}>
      <div className={styles.propsInner}>
        <h2 className={styles.propsHeading}>What you get out of the box</h2>
        <p className={styles.propsSubheading}>
          Six things the framework gives you on day one. Bring your own strategy.
        </p>
        <div className={styles.propsGrid}>
          {PROPS.map((p) => (
            <PropCard key={p.title} prop={p} />
          ))}
        </div>
      </div>
    </section>
  );
}

function PropCard({prop}: {prop: PropEntry}) {
  const url = useBaseUrl('/img/brand/' + prop.sprite);
  return (
    <div className={styles.prop}>
      <img src={url} alt="" className={styles.propIcon} aria-hidden />
      <h3>{prop.title}</h3>
      <p>{prop.body}</p>
    </div>
  );
}

function Cast() {
  return (
    <section className={styles.cast}>
      <div className={styles.castInner}>
        <h2 className={styles.propsHeading}>The Expedition</h2>
        <p className={styles.propsSubheading}>
          Permafrost uses a polar-camp metaphor for every moving part of the
          framework. Click any character to see what they do.
        </p>
        <div className={styles.castGrid}>
          {CAST.map((c) => (
            <CastCard key={c.name} entry={c} />
          ))}
        </div>
      </div>
    </section>
  );
}

function CastCard({entry}: {entry: CastEntry}) {
  const url = useBaseUrl('/img/brand/' + entry.sprite);
  return (
    <Link to={entry.href} className={styles.castCard}>
      <img src={url} alt={entry.name} />
      <div className={styles.castName}>{entry.name}</div>
      <div className={styles.castRole}>{entry.role}</div>
    </Link>
  );
}

function CTA() {
  return (
    <section className={styles.cta}>
      <h2>Ready to ship a strategy?</h2>
      <p>
        Five minutes from clone to a working scaffold. Write the logic; the framework
        handles the rest.
      </p>
      <div className={styles.heroButtons}>
        <Link
          className={`button button--lg ${styles.btnPrimary}`}
          to="/strategies/sapi">
          Write a strategy &rarr;
        </Link>
        <Link
          className={`button button--lg ${styles.btnSecondary}`}
          to="/strategies/scaffolding">
          permafrost strategy-new
        </Link>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  return (
    <Layout
      title="Permafrost -- your AI trading desk, locked in the ice"
      description="Open-source Go framework for self-custodied algorithmic trading with optional LLM augmentation. Hummingbot-style strategies, real killswitch, arctic-themed operator UI.">
      <HeroBanner />
      <QuickStart />
      <Props />
      <Cast />
      <CTA />
    </Layout>
  );
}
