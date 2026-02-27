// sb - Sandbox & worktree auditor
//
// Ensures git worktrees live under worktrees/ (not as siblings)
// and manages sandbox lifecycle for parallel agent workflows.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	Version   = "dev"
	GitCommit = "none"
	BuildDate = "unknown"
)

// isHelpFlag returns true if arg is -h or --help.
func isHelpFlag(arg string) bool {
	return arg == "-h" || arg == "--help"
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	var err error
	switch os.Args[1] {
	case "audit":
		err = runAudit()
	case "add":
		if len(os.Args) < 3 || isHelpFlag(os.Args[2]) {
			printAddUsage()
			if len(os.Args) >= 3 && isHelpFlag(os.Args[2]) {
				os.Exit(0)
			}
			os.Exit(1)
		}
		branch := ""
		if len(os.Args) > 3 {
			branch = os.Args[3]
		}
		err = runAdd(os.Args[2], branch)
	case "list":
		if len(os.Args) >= 3 && isHelpFlag(os.Args[2]) {
			printListUsage()
			os.Exit(0)
		}
		err = runList()
	case "remove":
		if len(os.Args) < 3 || isHelpFlag(os.Args[2]) {
			printRemoveUsage()
			if len(os.Args) >= 3 && isHelpFlag(os.Args[2]) {
				os.Exit(0)
			}
			os.Exit(1)
		}
		force := len(os.Args) > 3 && os.Args[3] == "--force"
		err = runRemove(os.Args[2], force)
	case "prune":
		err = runPrune()
	case "doctor":
		err = runDoctor()
	case "quickstart":
		runQuickstart()
	case "version":
		fmt.Printf("sb %s (commit: %s, built: %s)\n", Version, GitCommit, BuildDate)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`sb — Sandbox & worktree auditor

Ensures git worktrees live under worktrees/ (not as siblings).

Commands:
  quickstart       Setup instructions for LLM agents
  audit            Check all worktrees are under worktrees/
  add <name> [br]  Create a worktree under worktrees/
  list             List worktrees with placement status
  remove <name>    Remove a worktree from worktrees/
  prune            Clean up stale worktree references
  doctor           Run health checks on worktree setup
  version          Print version information
  help             Show this help

Use "sb <command> --help" for more information about a command.`)
}

func printAddUsage() {
	fmt.Println(`sb add — Create a worktree under worktrees/

Usage:
  sb add <name> [branch]

Arguments:
  <name>     Name for the worktree directory (required)
  [branch]   Branch name to use (optional, defaults to feat/<name>)

Examples:
  sb add my-feature              # creates worktrees/my-feature on feat/my-feature
  sb add bugfix fix/issue-123    # creates worktrees/bugfix on fix/issue-123

The command will:
  - Create worktrees/ directory if it doesn't exist
  - Add worktrees/ to .gitignore if not present
  - Track remote branch if origin/<branch> exists
  - Use existing local branch if it exists
  - Create a new branch otherwise`)
}

func printListUsage() {
	fmt.Println(`sb list — List worktrees with placement status

Usage:
  sb list

Output columns:
  STATUS     Placement status (main, ok, or MISPLACED)
  PATH       Relative path from repo root
  COMMIT     Short commit hash
  BRANCH     Branch name or (detached)

Status values:
  main       The main worktree (repo root)
  ok         Worktree correctly placed under worktrees/
  MISPLACED  Worktree outside worktrees/ (violation)`)
}

func printRemoveUsage() {
	fmt.Println(`sb remove — Remove a worktree from worktrees/

Usage:
  sb remove <name> [--force]

Arguments:
  <name>     Name of the worktree directory to remove (required)
  --force    Force removal even if worktree has uncommitted changes

Examples:
  sb remove my-feature           # remove worktrees/my-feature
  sb remove my-feature --force   # force remove even with uncommitted changes

Note: This only removes worktrees under the worktrees/ directory.`)
}

// repoRoot returns the git toplevel for cwd.
func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

// worktreeDir returns the canonical worktrees/ path inside the repo.
func worktreeDir() (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "worktrees"), nil
}

// worktreeEntry represents a parsed worktree from git worktree list.
type worktreeEntry struct {
	Path   string
	Commit string
	Branch string
	Bare   bool
}

func gitWorktreeList() ([]worktreeEntry, error) {
	out, err := exec.Command("git", "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil, err
	}
	var entries []worktreeEntry
	var cur worktreeEntry
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			if cur.Path != "" {
				entries = append(entries, cur)
			}
			cur = worktreeEntry{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "HEAD "):
			cur.Commit = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(line, "branch ")
		case line == "bare":
			cur.Bare = true
		}
	}
	if cur.Path != "" {
		entries = append(entries, cur)
	}
	return entries, nil
}

func runAudit() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	wtDir := filepath.Join(root, "worktrees")

	entries, err := gitWorktreeList()
	if err != nil {
		return err
	}

	var violations []worktreeEntry
	for _, e := range entries {
		if e.Path == root {
			continue
		}
		if !strings.HasPrefix(e.Path, wtDir+"/") {
			violations = append(violations, e)
		}
	}

	if len(violations) == 0 {
		fmt.Printf("ok: all %d worktrees are under worktrees/\n", len(entries)-1)
		return nil
	}

	fmt.Fprintf(os.Stderr, "VIOLATION: %d worktree(s) outside worktrees/:\n", len(violations))
	for _, v := range violations {
		rel, _ := filepath.Rel(root, v.Path)
		branch := v.Branch
		if branch == "" {
			branch = "(detached)"
		}
		fmt.Fprintf(os.Stderr, "  %s  %s  %s\n", rel, v.Commit[:8], branch)
	}
	fmt.Fprintf(os.Stderr, "\nFix with: git worktree move <worktree> worktrees/<name>\n")
	return fmt.Errorf("%d violation(s) found", len(violations))
}

func runAdd(name, branch string) error {
	wtDir, err := worktreeDir()
	if err != nil {
		return err
	}

	target := filepath.Join(wtDir, name)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("worktree %s already exists", target)
	}

	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		return err
	}

	ensureGitignore(wtDir)

	if branch == "" {
		branch = "feat/" + name
	}

	// Check if remote tracking branch exists.
	remoteBranch := "origin/" + branch
	if err := exec.Command("git", "rev-parse", "--verify", remoteBranch).Run(); err == nil {
		fmt.Printf("Tracking remote branch %s\n", remoteBranch)
		cmd := exec.Command("git", "worktree", "add", target, "--track", "-b", branch, remoteBranch)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Check if local branch exists.
	if err := exec.Command("git", "rev-parse", "--verify", branch).Run(); err == nil {
		fmt.Printf("Using existing branch %s\n", branch)
		cmd := exec.Command("git", "worktree", "add", target, branch)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Create new branch.
	fmt.Printf("Creating new branch %s\n", branch)
	cmd := exec.Command("git", "worktree", "add", "-b", branch, target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ensureGitignore(wtDir string) {
	root, err := repoRoot()
	if err != nil {
		return
	}
	giPath := filepath.Join(root, ".gitignore")
	content, _ := os.ReadFile(giPath)
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "worktrees/" || strings.TrimSpace(line) == "worktrees" {
			return
		}
	}
	f, err := os.OpenFile(giPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		f.WriteString("\n")
	}
	f.WriteString("worktrees/\n")
	fmt.Println("Added worktrees/ to .gitignore")
}

func runList() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	wtDir := filepath.Join(root, "worktrees")

	entries, err := gitWorktreeList()
	if err != nil {
		return err
	}

	for _, e := range entries {
		rel, _ := filepath.Rel(root, e.Path)
		branch := e.Branch
		if branch == "" {
			branch = "(detached)"
		}
		commit := e.Commit
		if len(commit) > 8 {
			commit = commit[:8]
		}

		status := "main"
		if e.Path == root {
			// main worktree
		} else if strings.HasPrefix(e.Path, wtDir+"/") {
			status = "ok"
		} else {
			status = "MISPLACED"
		}

		fmt.Printf("%-10s %-40s %s  %s\n", status, rel, commit, branch)
	}
	return nil
}

func runPrune() error {
	cmd := exec.Command("git", "worktree", "prune", "-v")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runDoctor performs health checks on worktree setup.
func runDoctor() error {
	fmt.Println("sb doctor — health check")
	fmt.Println()

	var warnings, errors int

	// 1. Git repository check
	root, err := repoRoot()
	if err != nil {
		fmt.Println("x Git repository: not in a git repository")
		errors++
		// Can't continue without a repo
		fmt.Println()
		fmt.Printf("Overall: %d warning(s), %d error(s)\n", warnings, errors)
		return fmt.Errorf("not in a git repository")
	}
	fmt.Printf("+ Git repository: %s\n", root)

	// 2. Worktree placement check
	entries, err := gitWorktreeList()
	if err != nil {
		fmt.Println("x Worktree list: failed to get worktree list")
		errors++
	} else {
		wtDir := filepath.Join(root, "worktrees")
		var misplaced []worktreeEntry
		var properCount int
		for _, e := range entries {
			if e.Path == root {
				continue // main worktree
			}
			if strings.HasPrefix(e.Path, wtDir+"/") {
				properCount++
			} else {
				misplaced = append(misplaced, e)
			}
		}

		if len(misplaced) == 0 {
			if properCount == 0 {
				fmt.Println("+ Worktree placement: no worktrees (main only)")
			} else {
				fmt.Printf("+ Worktree placement: all %d worktree(s) under worktrees/\n", properCount)
			}
		} else {
			fmt.Printf("x Worktree placement: %d worktree(s) outside worktrees/\n", len(misplaced))
			for _, m := range misplaced {
				rel, _ := filepath.Rel(root, m.Path)
				fmt.Printf("    - %s\n", rel)
			}
			fmt.Println("  Fix: git worktree move <path> worktrees/<name>")
			errors++
		}
	}

	// 3. Stale refs check
	staleRefs, err := checkStaleRefs()
	if err != nil {
		fmt.Println("! Stale refs: could not check")
		warnings++
	} else if len(staleRefs) == 0 {
		fmt.Println("+ Stale refs: none found")
	} else {
		fmt.Printf("! Stale refs: %d found\n", len(staleRefs))
		for _, ref := range staleRefs {
			fmt.Printf("    - %s\n", ref)
		}
		fmt.Println("  Fix: sb prune")
		warnings++
	}

	// 4. gitignore check
	gitignoreOk, gitignorePath := checkGitignore(root)
	if gitignoreOk {
		fmt.Println("+ gitignore: worktrees/ is ignored")
	} else {
		fmt.Println("! gitignore: worktrees/ not in .gitignore")
		fmt.Printf("  Fix: echo 'worktrees/' >> %s\n", gitignorePath)
		warnings++
	}

	// 5. Orphaned directories check
	wtDir := filepath.Join(root, "worktrees")
	orphans, err := checkOrphanedDirs(wtDir, entries)
	if err != nil {
		// worktrees/ might not exist, that's ok
		if !os.IsNotExist(err) {
			fmt.Println("! Orphaned directories: could not check")
			warnings++
		} else {
			fmt.Println("+ Orphaned directories: worktrees/ does not exist (ok)")
		}
	} else if len(orphans) == 0 {
		fmt.Println("+ Orphaned directories: none found")
	} else {
		fmt.Printf("! Orphaned directories: %d found in worktrees/\n", len(orphans))
		for _, o := range orphans {
			fmt.Printf("    - %s\n", o)
		}
		fmt.Println("  Fix: rm -rf worktrees/<name> (after verifying no uncommitted work)")
		warnings++
	}

	fmt.Println()
	fmt.Printf("Overall: %d warning(s), %d error(s)\n", warnings, errors)

	if errors > 0 {
		return fmt.Errorf("%d error(s) found", errors)
	}
	return nil
}

// checkStaleRefs checks for stale worktree references.
func checkStaleRefs() ([]string, error) {
	// git worktree prune --dry-run shows what would be pruned
	out, err := exec.Command("git", "worktree", "prune", "--dry-run", "-v").CombinedOutput()
	if err != nil {
		return nil, err
	}

	var stale []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.Contains(line, "Removing") {
			stale = append(stale, line)
		}
	}
	return stale, nil
}

// checkGitignore checks if worktrees/ is in .gitignore.
func checkGitignore(root string) (bool, string) {
	giPath := filepath.Join(root, ".gitignore")
	content, err := os.ReadFile(giPath)
	if err != nil {
		return false, giPath
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "worktrees/" || trimmed == "worktrees" || trimmed == "/worktrees/" || trimmed == "/worktrees" {
			return true, giPath
		}
	}
	return false, giPath
}

// checkOrphanedDirs finds directories in worktrees/ not registered with git.
func checkOrphanedDirs(wtDir string, entries []worktreeEntry) ([]string, error) {
	dirEntries, err := os.ReadDir(wtDir)
	if err != nil {
		return nil, err
	}

	// Build set of registered worktree paths
	registered := make(map[string]bool)
	for _, e := range entries {
		registered[e.Path] = true
	}

	var orphans []string
	for _, d := range dirEntries {
		if !d.IsDir() {
			continue
		}
		fullPath := filepath.Join(wtDir, d.Name())
		if !registered[fullPath] {
			orphans = append(orphans, d.Name())
		}
	}
	return orphans, nil
}

func runQuickstart() {
	root, _ := repoRoot()
	rootInfo := "(not in a git repo)"
	if root != "" {
		rootInfo = root
	}

	fmt.Printf(`# sb quickstart — agent context
# version: %s (%s)
# repo root: %s

## what sb does

sb enforces the convention that git worktrees live under worktrees/
inside the repository, not as sibling directories. This keeps ghq
layouts clean and lets agents discover each other's in-flight work
via git branch -r without confusing worktrees for separate repos.

## ecosystem

sb is part of a multi-agent tooling ecosystem:

  bd  (beads)   — issue tracking with hash-based IDs
  gt  (gastown) — multi-agent orchestration with rig abstraction
  cprr          — conjecture-proof-refutation-refinement; worktrees/ gitignored, shell scripts
  sb            — sandbox & worktree auditor (this tool)

all tools install to ~/.local/bin and use gmake on FreeBSD.

## typical agent workflow

  sb doctor                 # health check (stale refs, gitignore, orphans)
  sb audit                  # verify no misplaced worktrees
  sb add my-feature         # creates worktrees/my-feature on feat/my-feature
  cd worktrees/my-feature   # isolated working directory
  # ... make changes, commit, push ...
  sb remove my-feature      # clean up when done
  sb prune                  # remove stale refs

## setup (run these commands)

  ghq get jwalsh/sb
  cd ~/ghq/github.com/jwalsh/sb
  make install              # or: gmake install (FreeBSD)
  # override go binary: GO=go124 gmake install
  # skip freshness check:  SKIP_UPDATE_CHECK=1 gmake install

## verify

  sb version
  sb audit
`, Version, GitCommit, rootInfo)
}

func runRemove(name string, force bool) error {
	wtDir, err := worktreeDir()
	if err != nil {
		return err
	}
	target := filepath.Join(wtDir, name)
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return fmt.Errorf("worktree %s does not exist", name)
	}

	args := []string{"worktree", "remove", target}
	if force {
		args = append(args, "--force")
	}
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
