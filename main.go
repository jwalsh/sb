// sb - Sandbox & worktree auditor
//
// Ensures git worktrees live under worktrees/ (not as siblings)
// and manages sandbox lifecycle for parallel agent workflows.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	Version    = "dev"
	GitCommit  = "none"
	BuildDate  = "unknown"
	jsonOutput bool
)

// isHelpFlag returns true if arg is -h or --help.
func isHelpFlag(arg string) bool {
	return arg == "-h" || arg == "--help"
}

func main() {
	// Pre-parse --json flag and remove it from args
	var filtered []string
	for _, arg := range os.Args[1:] {
		if arg == "--json" {
			jsonOutput = true
		} else {
			filtered = append(filtered, arg)
		}
	}
	os.Args = append([]string{os.Args[0]}, filtered...)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	var err error
	switch os.Args[1] {
	case "audit":
		err = runAudit()
	case "add":
		args := os.Args[2:]
		// Skip -- separator if present (POSIX convention for end of flags)
		dashDashUsed := false
		if len(args) > 0 && args[0] == "--" {
			dashDashUsed = true
			args = args[1:]
		}
		if len(args) < 1 || isHelpFlag(args[0]) {
			printAddUsage()
			if len(args) >= 1 && isHelpFlag(args[0]) {
				os.Exit(0)
			}
			os.Exit(1)
		}
		name := args[0]
		branch := ""
		if len(args) > 1 {
			branch = args[1]
		}
		err = runAdd(name, branch, dashDashUsed)
	case "list":
		if len(os.Args) >= 3 && isHelpFlag(os.Args[2]) {
			printListUsage()
			os.Exit(0)
		}
		err = runList()
	case "remove":
		args := os.Args[2:]
		// Skip -- separator if present (POSIX convention for end of flags)
		dashDashUsed := false
		if len(args) > 0 && args[0] == "--" {
			dashDashUsed = true
			args = args[1:]
		}
		if len(args) < 1 || isHelpFlag(args[0]) {
			printRemoveUsage()
			if len(args) >= 1 && isHelpFlag(args[0]) {
				os.Exit(0)
			}
			os.Exit(1)
		}
		name := args[0]
		force := len(args) > 1 && args[1] == "--force"
		err = runRemove(name, force, dashDashUsed)
	case "prune":
		err = runPrune()
	case "doctor":
		err = runDoctor()
	case "quickstart", "prime":
		runQuickstart()
	case "init":
		err = runInit()
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
  prime            Agent bootstrap context (alias: quickstart)
  init             Initialize worktrees/ directory and gitignore
  audit            Check all worktrees are under worktrees/
  add <name> [br]  Create a worktree under worktrees/
  list             List worktrees with placement status
  remove <name>    Remove a worktree from worktrees/
  prune            Clean up stale worktree references
  doctor           Run health checks on worktree setup
  version          Print version information
  help             Show this help

Global flags:
  --json           Output in JSON format (for agent consumption)

Use "sb <command> --help" for more information about a command.`)
}

func printAddUsage() {
	fmt.Println(`sb add — Create a worktree under worktrees/

Usage:
  sb add [--] <name> [branch]

Arguments:
  <name>     Name for the worktree directory (required)
  [branch]   Branch name to use (optional, defaults to feat/<name>)

Options:
  --         End of flags separator. Use when <name> starts with a dash.

Examples:
  sb add my-feature              # creates worktrees/my-feature on feat/my-feature
  sb add bugfix fix/issue-123    # creates worktrees/bugfix on fix/issue-123
  sb add -- --weird-name         # creates worktrees/--weird-name (name starts with dash)

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
  sb remove [--] <name> [--force]

Arguments:
  <name>     Name of the worktree directory to remove (required)

Options:
  --         End of flags separator. Use when <name> starts with a dash.
  --force    Force removal even if worktree has uncommitted changes

Examples:
  sb remove my-feature           # remove worktrees/my-feature
  sb remove my-feature --force   # force remove even with uncommitted changes
  sb remove -- --weird-name      # remove worktrees/--weird-name (name starts with dash)

Note: This only removes worktrees under the worktrees/ directory.`)
}

// validateWorktreeName checks for pathological worktree/branch names that could
// cause security issues or unexpected behavior. Returns an error with actionable
// guidance if the name is invalid. If skipDashCheck is true, dash-prefixed names
// are allowed (used when -- separator was provided).
func validateWorktreeName(name string, skipDashCheck bool) error {
	// Empty or whitespace-only
	if name == "" {
		return fmt.Errorf("worktree name cannot be empty")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("worktree name cannot be whitespace-only")
	}

	// Starts with dash (looks like a flag) - skip if -- separator was used
	if !skipDashCheck && strings.HasPrefix(name, "-") {
		return fmt.Errorf("worktree name %q starts with dash; use 'sb <command> -- %s' to force", name, name)
	}

	// Path traversal attacks
	if name == "." || name == ".." {
		return fmt.Errorf("worktree name cannot be %q (path traversal)", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("worktree name %q contains '..' (path traversal not allowed)", name)
	}
	if strings.Contains(name, "./") || strings.Contains(name, "/.") {
		return fmt.Errorf("worktree name %q contains path traversal pattern", name)
	}
	if strings.HasPrefix(name, "/") || strings.Contains(name, "/") {
		return fmt.Errorf("worktree name %q contains '/' (use simple names, not paths)", name)
	}

	// Shell metacharacters that could enable injection
	shellMetachars := []string{"$", "`", "|", ";", "&", ">", "<", "*", "?", "(", ")", "[", "]", "{", "}", "!", "~", "'", "\"", "\\"}
	for _, c := range shellMetachars {
		if strings.Contains(name, c) {
			return fmt.Errorf("worktree name %q contains shell metacharacter %q (potential injection)", name, c)
		}
	}

	// Git reserved names
	if strings.ToUpper(name) == "HEAD" {
		return fmt.Errorf("worktree name %q is reserved by git", name)
	}
	if strings.HasPrefix(name, "refs/") || strings.HasPrefix(name, "refs\\") {
		return fmt.Errorf("worktree name %q starts with refs/ (reserved by git)", name)
	}

	// Control characters (ASCII < 32) and null bytes
	for i := 0; i < len(name); i++ {
		if name[i] < 32 || name[i] == 127 {
			return fmt.Errorf("worktree name contains control character at position %d (ASCII %d)", i, name[i])
		}
	}

	// Length limit
	if len(name) > 255 {
		return fmt.Errorf("worktree name is too long (%d chars, max 255)", len(name))
	}

	return nil
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

// auditOutput represents the JSON output for sb audit.
type auditOutput struct {
	RepoRoot       string         `json:"repo_root"`
	Status         string         `json:"status"`
	WorktreeCount  int            `json:"worktree_count"`
	ViolationCount int            `json:"violation_count"`
	Violations     []worktreeJSON `json:"violations,omitempty"`
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

	if jsonOutput {
		output := auditOutput{
			RepoRoot:       root,
			WorktreeCount:  len(entries) - 1, // exclude main
			ViolationCount: len(violations),
		}

		if len(violations) == 0 {
			output.Status = "ok"
		} else {
			output.Status = "violation"
			output.Violations = make([]worktreeJSON, 0, len(violations))
			for _, v := range violations {
				rel, _ := filepath.Rel(root, v.Path)
				branch := v.Branch
				if branch == "" {
					branch = "(detached)"
				}
				commit := v.Commit
				if len(commit) > 8 {
					commit = commit[:8]
				}
				output.Violations = append(output.Violations, worktreeJSON{
					Name:   filepath.Base(v.Path),
					Path:   rel,
					Branch: branch,
					Commit: commit,
					Status: "MISPLACED",
				})
			}
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(output); err != nil {
			return err
		}

		if len(violations) > 0 {
			return fmt.Errorf("%d violation(s) found", len(violations))
		}
		return nil
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

func runAdd(name, branch string, skipDashCheck bool) error {
	// Validate worktree name before any filesystem operations
	if err := validateWorktreeName(name, skipDashCheck); err != nil {
		return err
	}

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

// worktreeJSON represents a worktree in JSON output.
type worktreeJSON struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	Status string `json:"status"`
}

// listOutput represents the JSON output for sb list.
type listOutput struct {
	RepoRoot  string         `json:"repo_root"`
	Worktrees []worktreeJSON `json:"worktrees"`
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

	if jsonOutput {
		output := listOutput{
			RepoRoot:  root,
			Worktrees: make([]worktreeJSON, 0, len(entries)),
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

			// Extract name from path for worktrees under worktrees/
			name := rel
			if strings.HasPrefix(rel, "worktrees/") {
				name = strings.TrimPrefix(rel, "worktrees/")
			}

			output.Worktrees = append(output.Worktrees, worktreeJSON{
				Name:   name,
				Path:   rel,
				Branch: branch,
				Commit: commit,
				Status: status,
			})
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
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

// runInit initializes the worktrees/ directory and ensures it's gitignored.
func runInit() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}

	wtDir := filepath.Join(root, "worktrees")

	// Check if worktrees/ already exists
	if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
		fmt.Printf("worktrees/ already exists at %s\n", wtDir)
	} else {
		// Create worktrees/ directory
		if err := os.MkdirAll(wtDir, 0o755); err != nil {
			return fmt.Errorf("failed to create worktrees/: %w", err)
		}
		fmt.Printf("Created worktrees/ at %s\n", wtDir)
	}

	// Ensure worktrees/ is in .gitignore
	ensureGitignore(wtDir)

	// Create a .gitkeep so the directory is trackable if needed
	gitkeep := filepath.Join(wtDir, ".gitkeep")
	if _, err := os.Stat(gitkeep); os.IsNotExist(err) {
		if err := os.WriteFile(gitkeep, []byte(""), 0o644); err != nil {
			return fmt.Errorf("failed to create .gitkeep: %w", err)
		}
	}

	fmt.Println("Initialized. Run 'sb doctor' to verify setup.")
	return nil
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

  bd    (beads)   — issue tracking with hash-based IDs
  gt    (gastown) — multi-agent orchestration with rig abstraction
  adtap           — activity pub server + digest agent
  cprr            — conjecture-proof-refutation-refinement
  sb              — sandbox & worktree auditor (this tool)

all tools install to ~/.local/bin and use gmake on FreeBSD.

## typical agent workflow

  sb init                   # set up worktrees/ and gitignore (first time)
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

func runRemove(name string, force bool, skipDashCheck bool) error {
	// Validate worktree name before any filesystem operations
	if err := validateWorktreeName(name, skipDashCheck); err != nil {
		return err
	}

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
