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
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: sb add <name> [branch]")
			os.Exit(1)
		}
		branch := ""
		if len(os.Args) > 3 {
			branch = os.Args[3]
		}
		err = runAdd(os.Args[2], branch)
	case "list":
		err = runList()
	case "remove":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: sb remove <name>")
			os.Exit(1)
		}
		force := len(os.Args) > 3 && os.Args[3] == "--force"
		err = runRemove(os.Args[2], force)
	case "prune":
		err = runPrune()
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
  version          Print version information
  help             Show this help`)
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
  cprr          — conjecture-proof-refutation-refinement methodology
  sb            — sandbox & worktree auditor (this tool)

all tools install to ~/.local/bin and use gmake on FreeBSD.

## typical agent workflow

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
