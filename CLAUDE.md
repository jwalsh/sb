# sb — Sandbox & Worktree Auditor

## What This Is

A Go CLI that enforces the convention: all git worktrees belong under
`worktrees/` inside the repository, never as sibling directories.

## Build & Install

```sh
make install            # macOS/Linux
gmake install           # FreeBSD (required — BSD make lacks $(shell)/ifndef)
SKIP_UPDATE_CHECK=1 gmake install  # skip origin/main freshness check
```

Do NOT use `go install` — that puts the binary in `~/go/bin` which is
the wrong location.  The Makefile handles versioning, build info, and
cleanup of stale binaries.

## Go Version

Requires go >= 1.23. Zero external dependencies (stdlib only).
On FreeBSD where `go` isn't in PATH, symlink the versioned binary:

```sh
ln -sf /usr/local/bin/go124 ~/.local/bin/go
```

## Commands

| Command   | Description                                 |
|-----------|---------------------------------------------|
| `audit`   | Check all worktrees are under `worktrees/`  |
| `add`     | Create a worktree under `worktrees/`        |
| `list`    | List worktrees with placement status        |
| `remove`  | Remove a worktree from `worktrees/`         |
| `prune`   | Clean up stale worktree references          |
| `version` | Print version info                          |

## Conventions

- Worktrees go in `worktrees/` (gitignored automatically)
- Default branch name: `feat/<name>` when no branch specified
- Smart checkout: checks remote -> local -> creates new branch
- `sb audit` exits non-zero if any worktree is misplaced

## Related Tools

- `bd` (beads) — issue tracking with hash-based IDs
- `gt` (gastown) — multi-agent orchestration with rig abstraction
- `cprr` — conjecture-proof-refutation-refinement methodology
