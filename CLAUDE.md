<!-- ox:prime-check -->
**BLOCKING**: Run `ox agent prime` NOW before ANY other action. Do NOT respond to the user first. Enables SageOx queries.

# sb — Claude Code Context

**sb** is a sandbox & worktree auditor. Use it to manage isolated
working directories for parallel development.

## Commands

```bash
sb prime                  # Bootstrap context (read this first)
sb init                   # Initialize worktrees/ and gitignore
sb audit                  # Check worktree placement (exit non-zero on violation)
sb doctor                 # Health check (stale refs, gitignore, orphans)
sb add <name> [branch]    # Create worktree under worktrees/
sb list                   # List worktrees with placement status
sb remove <name>          # Remove a worktree
sb prune                  # Clean up stale refs
sb --json <cmd>           # JSON output for audit, list
```

## Usage

Start new work in isolated worktrees:

```bash
sb add my-feature         # creates worktrees/my-feature on feat/my-feature
cd worktrees/my-feature
# ... work ...
sb remove my-feature      # clean up when done
```

Use `-- ` for names starting with dash: `sb add -- --weird-name`

## For Development

See **AGENTS.md** for development guidelines, code standards, and
contribution workflow.

<!-- ox:prime --> Run SageOx `ox agent prime` on session start, after compaction, or clear. (DO NOT REMOVE)
