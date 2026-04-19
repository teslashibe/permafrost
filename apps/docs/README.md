# Permafrost docs

The Permafrost documentation site, built with [Docusaurus 3](https://docusaurus.io/).

Lives in `apps/docs/` as a self-contained Node project alongside the Go code. Built and deployed by `.github/workflows/docs.yml` on pushes to `main`.

## Local development

```bash
cd apps/docs
npm install
npm start            # http://localhost:3000/permafrost/
```

`npm start` runs the Docusaurus dev server with hot reload.

## Build

```bash
npm run build        # generates static site into apps/docs/build/
npm run serve        # serves the built site locally
```

The build step also fails on broken cross-references and broken markdown links — useful as a pre-PR check.

## Adding a page

1. Create the markdown / mdx file under `docs/<section>/<page>.md`.
2. Add it to `sidebars.ts`.
3. Reference it from related pages with relative links.
4. `npm run build` to verify nothing broke.

The sidebar lives in `sidebars.ts`. Sections are categories with a flat list of items; reorder via `sidebar_position` frontmatter on each page.

## Deployment

Pushed to GitHub Pages by `.github/workflows/docs.yml` whenever `apps/docs/**` changes on `main`. The deployed URL is `https://teslashibe.github.io/permafrost/` until a custom domain is wired (update `url` and `baseUrl` in `docusaurus.config.ts` when that happens).

## Path filters

The Go CI workflow (`.github/workflows/ci.yml`) ignores `apps/docs/**` so docs-only PRs don't trigger Go test runs. Conversely, this workflow only runs on `apps/docs/**` changes.
