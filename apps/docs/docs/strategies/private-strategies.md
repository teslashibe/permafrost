---
sidebar_position: 4
---

# Private strategies

You don't have to publish a strategy to run it. The repo is laid out so you can drop a private strategy into your local clone, build, and run — without forking, without managing a second repo, and without leaking it to upstream.

## The convention

```
strategies/
├── noop/                          ← committed, public
├── <community_strategy>/          ← committed, public
└── private/                       ← gitignored as a directory
    ├── my_basis/                  ← private, never pushed
    └── my_market_maker/
```

`.gitignore` excludes `/strategies/private/` as a whole directory. Anything you put under it is invisible to git.

## Enabling private strategies in your build

`cmd/permafrostd/strategies.go` (committed) blank-imports community strategies.

`cmd/permafrostd/strategies_local.go` (gitignored) blank-imports your private ones:

```go title="cmd/permafrostd/strategies_local.go"
package main

import (
    _ "github.com/teslashibe/permafrost/strategies/private/my_basis"
    _ "github.com/teslashibe/permafrost/strategies/private/my_market_maker"
)
```

Both files are in `package main` and compile into the same binary. The framework's registry sees both sets at startup. Build:

```bash
go build -o bin/permafrostd ./cmd/permafrostd
```

To run an OSS-only build (no private strategies), delete `strategies_local.go`. To remove a single private strategy from a build, delete its line.

## Backups (you have to think about this)

Anything under `strategies/private/` is gitignored. That means it isn't backed up by your normal `git push` flow. Three failure modes to plan for:

1. **`git clean -fdx` wipes untracked files.** You will eventually run this command and forget what's in `strategies/private/`.
2. **`rm -rf` mistakes.** Standard.
3. **Disk failure.** Standard.

Pick at least one mitigation:

- **Time Machine / Backblaze / equivalent.** Easiest. Make sure `strategies/private/` is *not* in your backup-exclude list.
- **Periodic tarball.** `tar -czf ~/backup/strategies-$(date +%F).tar.gz strategies/private/` in a cron / launchd job.
- **Private fork on a separate branch.** Maintain a `private` branch in your own fork that includes `strategies/private/` and `cmd/permafrostd/strategies_local.go`. Push to that branch only. Never to `main`. Most invasive but gives you full git history.

For trading code, plan **at least two** of the above. Strategies that lose money are sad; strategies that lose themselves are worse.

## Avoiding accidental leaks

Three failure modes:

1. **`git add -f`** — bypasses `.gitignore`. Easy to do by accident with a wildcard. A pre-commit hook is good insurance:

   ```bash title=".git/hooks/pre-commit"
   #!/bin/sh
   if git diff --cached --name-only | grep -E '^(strategies/private/|cmd/permafrostd/strategies_local\.go$)' > /dev/null; then
       echo "ERROR: refusing to commit private strategy paths"
       git diff --cached --name-only | grep -E '^(strategies/private/|cmd/permafrostd/strategies_local\.go$)'
       exit 1
   fi
   ```

2. **IDE auto-stage.** Some setups stage all changes. Check yours.

3. **Branch pushes.** A branch that includes a stash apply may carry private files. `git status` before every push.

## Sharing a private strategy across machines

If you run `permafrostd` on more than one machine (laptop + server), you'll want to sync. Options in increasing rigor:

- **scp / rsync.** Two commands. Drift-prone.
- **Private Git repo for `strategies/private/` only.** Initialize a separate repo *inside* `strategies/private/`, push it to a private remote. The outer repo's `.gitignore` keeps it out of public push.
- **Private fork.** As above in the backup section. One repo, two branches: `main` (clean OSS, syncs upstream) and `private` (your build, never pushed to upstream).

## Promoting a private strategy to public

When you decide a strategy is good enough to share:

```bash
mv strategies/private/my_strategy strategies/my_strategy
# remove the import line from strategies_local.go and add it to strategies.go
git add strategies/my_strategy/ cmd/permafrostd/strategies.go cmd/permafrostd/strategies_local.go
git commit -m "Promote my_strategy to public"
```

Open a PR upstream if you want others to use it. The framework doesn't care either way.

## Next steps

- [Testing](/strategies/testing)
- [SAPI overview](/reference/sapi-overview)
