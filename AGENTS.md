# Agent Instructions for sb Development

**sb** is a sandbox & worktree auditor. It enforces the convention that
git worktrees live under `worktrees/` inside the repository, not as
sibling directories. This matters because sibling worktrees break `ghq`
layouts and make it impossible for agents to distinguish worktrees from
separate repos.

**Read this entire file before writing any code.**

---

## Ecosystem Context

sb is part of a multi-agent tooling ecosystem. These tools are designed
for agents, not people. Every tool installs to `~/.local/bin` and
builds with `make` (or `gmake` on FreeBSD).

| Tool   | Repo                     | Purpose                                    |
|--------|--------------------------|--------------------------------------------|
| `bd`   | steveyegge/beads         | Distributed issue tracking with hash IDs   |
| `gt`   | steveyegge/gastown       | Multi-agent orchestration with rigs        |
| `cprr` | jwalsh/cprr              | Conjecture-proof-refutation-refinement     |
| `sb`   | jwalsh/sb *(this repo)*  | Sandbox & worktree auditor                 |

**Do NOT create issues, PRs, or forks on `steveyegge/*` repos.** Those
are read-only references. Work happens here.

---

## Quick Reference

```bash
sb quickstart             # Agent-consumable setup context
sb audit                  # Check worktree placement (exits non-zero on violations)
sb add <name> [branch]    # Create worktree under worktrees/
sb list                   # List worktrees with placement status
sb remove <name>          # Remove a worktree
sb prune                  # Clean up stale refs
sb version                # Print version info
```

---

## Development Guidelines

### Code Standards

- **Go version**: >= 1.23 (tested with 1.24 on FreeBSD)
- **Dependencies**: Zero. Stdlib only. Do NOT add external dependencies
  without strong justification and explicit approval.
- **Build**: `make build` (or `GO=go124 gmake build` on FreeBSD)
- **Test**: `make test`
- **Lint**: `make lint` (go vet + gofmt check + golangci-lint if available)
- **Format**: `make fmt`

### File Organization

```
sb/
├── main.go              # All CLI logic (single-file for now)
├── go.mod               # Module definition (no dependencies)
├── Makefile             # GNU make (gmake on FreeBSD)
├── sb.org               # Literate org-babel source (canonical design doc)
├── README.org           # Project documentation
├── CLAUDE.md            # Agent quick-reference
├── AGENTS.md            # This file — development guide
├── LICENSE              # MIT
└── .gitignore           # sb binary + worktrees/
```

### CLI Design Principles

**Minimize cognitive overhead.** sb is a small tool. Keep it that way.

1. **No frameworks.** stdlib `os.Args` switch, not cobra/urfave/kong.
   The dependency tree is the enemy. mousetrap getting purged from the
   Go proxy proved this.

2. **No flags where positional args suffice.** `sb add my-feature` not
   `sb add --name my-feature`. The only exception is `--force` on remove.

3. **Exit codes matter.** `sb audit` returns non-zero on violations.
   Agents and CI depend on this. Do not swallow errors.

4. **No interactive prompts.** Agents cannot answer y/n questions. Every
   command must run non-interactively. If confirmation is needed, require
   `--force` instead.

5. **`quickstart` is for agents.** The output of `sb quickstart` is
   structured text an LLM can consume to bootstrap itself. Keep it
   current when adding commands.

### Adding a New Command

1. Add a case to the `switch` in `main()`
2. Add the command to `printUsage()`
3. Add it to `runQuickstart()` if agents need to know about it
4. Update README.org and CLAUDE.md
5. Add to the Makefile `help` target if it has a make wrapper
6. Write tests

### Testing

```bash
make test                 # Unit tests
make test-race            # Race detector
```

**IMPORTANT:** sb operates on git worktrees, so integration tests need
a real git repo. Use `t.TempDir()` with `git init` for test fixtures.
Do NOT pollute the development repo with test worktrees.

### Cross-Platform

sb must build and run on:

- **darwin/arm64** — primary dev (jwalsh)
- **darwin/amd64** — CI
- **freebsd/amd64** — dsp-dr's box
- **linux/amd64** — CI, containers

Verify with `make build-all`. Do not use platform-specific syscalls.

---

## Non-Interactive Shell Commands

**ALWAYS use non-interactive flags** to avoid hanging on confirmation
prompts. Some systems alias `cp`, `mv`, `rm` to include `-i`.

```bash
cp -f source dest         # NOT: cp source dest
mv -f source dest         # NOT: mv source dest
rm -f file                # NOT: rm file
```

---

## Eating Our Own Dogfood

sb enforces the `worktrees/` convention. When developing sb itself,
**use `worktrees/` for feature branches:**

```bash
sb add my-feature                  # creates worktrees/my-feature
cd worktrees/my-feature
# ... develop ...
sb audit                           # should pass
git push -u origin feat/my-feature
sb remove my-feature
```

If `sb audit` fails in this repo, that is a P0 bug.

---

## Before Committing

1. **Build**: `make build`
2. **Test**: `make test`
3. **Lint**: `make lint`
4. **Audit**: `./sb audit` (eat your own dogfood)
5. **Quickstart**: `./sb quickstart` (verify it's still accurate)

Use conventional commits:

```
feat: add sync command
fix: handle detached HEAD in audit
docs: update AGENTS.md with new command
chore: bump go version in go.mod
```

---

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work
is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **Run quality gates** (if code changed):
   ```bash
   make lint
   make test
   ./sb audit
   ```
2. **Commit** with conventional commit message
3. **PUSH TO REMOTE** — This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
4. **Clean up** — Clear stashes, prune remote branches
5. **Verify** — All changes committed AND pushed
6. **Hand off** — Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing — that leaves work stranded locally
- NEVER say "ready to push when you are" — YOU must push
- If push fails, resolve and retry until it succeeds
- Multiple agents coordinate via pushed branches. Unpushed work is invisible.

---

## Version Management

Version is embedded at build time via ldflags:

```bash
make version-info         # Show VERSION, COMMIT, BUILD_DATE
```

To tag a release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The Makefile extracts version from `git describe --tags --always --dirty`.

---

## Future Work

Track work in GitHub issues on `jwalsh/sb`. Planned commands:

- `sb sync` — Fetch remote, show available branches for checkout
- `sb init` — Initialize `worktrees/` with gitignore + CLAUDE.md overlay
- `sb move` — Relocate misplaced worktrees into `worktrees/`
- `sb doctor` — Health check (stale refs, misplaced trees, gitignore)
- JSON output mode (`--json`) for agent consumption

When implementing these, follow the existing pattern: add to `main()`
switch, update `printUsage()`, update `runQuickstart()`, write tests.

---

## Questions?

- Check the literate source: `sb.org`
- Run `sb quickstart` for current state
- Look at recent commits: `git log --oneline -20`
- Check beads/gastown for ecosystem conventions (read-only)
