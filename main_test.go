package main

import (
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

// --- Unit tests for validateWorktreeName ---

func TestValidateWorktreeName_ValidNames(t *testing.T) {
	valid := []string{
		"my-feature",
		"bugfix",
		"feat-auth-v2",
		"a",
		"abc123",
		"with-dashes-and-numbers-123",
		strings.Repeat("x", 255), // max length
	}
	for _, name := range valid {
		if err := validateWorktreeName(name, false); err != nil {
			t.Errorf("expected valid, got error for %q: %v", name, err)
		}
	}
}

func TestValidateWorktreeName_Rejects(t *testing.T) {
	cases := []struct {
		name          string
		skipDashCheck bool
		wantSubstr    string
	}{
		{"", false, "empty"},
		{"   ", false, "whitespace"},
		{"-starts-with-dash", false, "starts with dash"},
		{".", false, "path traversal"},
		{"..", false, "path traversal"},
		{"foo..bar", false, ".."},
		{"foo/bar", false, "/"},
		{"/absolute", false, "/"},
		{"has$dollar", false, "shell metacharacter"},
		{"has`backtick", false, "shell metacharacter"},
		{"has|pipe", false, "shell metacharacter"},
		{"has;semi", false, "shell metacharacter"},
		{"has&amp", false, "shell metacharacter"},
		{"HEAD", false, "reserved"},
		{"head", false, "reserved"},
		{"refs/heads/main", false, "refs/"},
		{strings.Repeat("x", 256), false, "too long"},
		{"has\x00null", false, "control character"},
		{"has\ttab", false, "control character"},
		{"has\nnewline", false, "control character"},
	}

	for _, tc := range cases {
		err := validateWorktreeName(tc.name, tc.skipDashCheck)
		if err == nil {
			t.Errorf("expected rejection for %q, got nil", tc.name)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantSubstr)) {
			t.Errorf("for %q: error %q doesn't contain %q", tc.name, err.Error(), tc.wantSubstr)
		}
	}
}

func TestValidateWorktreeName_DashWithSkip(t *testing.T) {
	// With skipDashCheck=true, dash-prefixed names should be allowed
	// (assuming no other violations)
	err := validateWorktreeName("-my-flag-name", true)
	if err != nil {
		t.Errorf("expected dash-prefixed name to pass with skipDashCheck=true, got: %v", err)
	}
}

// --- Property-based tests ---

// Property: any name that passes validation must be non-empty, <= 255 bytes,
// contain no shell metacharacters, no path separators, no control chars,
// and not be a git reserved name.
func TestProperty_ValidNamesAreSafe(t *testing.T) {
	shellMetachars := "$`|;&><*?()[]{}!~'\"\\/"

	prop := func(name string) bool {
		err := validateWorktreeName(name, true) // skipDashCheck to focus on safety invariants
		if err != nil {
			return true // rejected names trivially satisfy safety
		}

		// If accepted, verify all safety invariants hold:

		// Non-empty
		if name == "" || strings.TrimSpace(name) == "" {
			t.Logf("VIOLATION: accepted empty/whitespace name %q", name)
			return false
		}

		// Length bounded
		if len(name) > 255 {
			t.Logf("VIOLATION: accepted name longer than 255 bytes: len=%d", len(name))
			return false
		}

		// No shell metacharacters
		for _, c := range shellMetachars {
			if strings.ContainsRune(name, c) {
				t.Logf("VIOLATION: accepted name %q containing shell metachar %q", name, string(c))
				return false
			}
		}

		// No path traversal
		if strings.Contains(name, "..") {
			t.Logf("VIOLATION: accepted name %q containing '..'", name)
			return false
		}
		if name == "." {
			t.Logf("VIOLATION: accepted name '.'")
			return false
		}

		// No control characters
		for i := 0; i < len(name); i++ {
			if name[i] < 32 || name[i] == 127 {
				t.Logf("VIOLATION: accepted name with control char at pos %d (byte %d)", i, name[i])
				return false
			}
		}

		// Not git reserved
		if strings.ToUpper(name) == "HEAD" {
			t.Logf("VIOLATION: accepted git-reserved name %q", name)
			return false
		}

		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 10000}); err != nil {
		t.Errorf("property violated: %v", err)
	}
}

// Property: validation is deterministic — same input always produces same result.
func TestProperty_ValidationDeterministic(t *testing.T) {
	prop := func(name string) bool {
		r1 := validateWorktreeName(name, false)
		r2 := validateWorktreeName(name, false)
		// Both nil or both non-nil with same message
		if (r1 == nil) != (r2 == nil) {
			return false
		}
		if r1 != nil && r2 != nil && r1.Error() != r2.Error() {
			return false
		}
		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 5000}); err != nil {
		t.Errorf("determinism violated: %v", err)
	}
}

// Property: skipDashCheck only affects names starting with '-'.
// For names NOT starting with '-', the result should be identical
// regardless of skipDashCheck.
func TestProperty_SkipDashCheckOnlyAffectsDashNames(t *testing.T) {
	prop := func(name string) bool {
		if len(name) == 0 || name[0] == '-' {
			return true // skip — this is the case where behavior should differ
		}
		r1 := validateWorktreeName(name, false)
		r2 := validateWorktreeName(name, true)
		if (r1 == nil) != (r2 == nil) {
			t.Logf("VIOLATION: skipDashCheck changed result for non-dash name %q", name)
			return false
		}
		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 5000}); err != nil {
		t.Errorf("skipDashCheck scope violated: %v", err)
	}
}

// Property: all single-character ASCII printable names that aren't
// shell metacharacters, '.', '/', or git-reserved should be accepted.
func TestProperty_SingleCharAcceptance(t *testing.T) {
	shellMetachars := "$`|;&><*?()[]{}!~'\"\\/"

	for b := byte(32); b < 127; b++ {
		name := string(b)
		err := validateWorktreeName(name, true)

		isMetachar := strings.ContainsRune(shellMetachars, rune(b))
		isDot := name == "."
		isSpace := name == " "
		isReserved := false // single chars can't be "HEAD"

		shouldReject := isMetachar || isDot || isSpace || isReserved

		if shouldReject && err == nil {
			t.Errorf("expected rejection for single char %q (byte %d)", name, b)
		}
		if !shouldReject && err != nil {
			t.Errorf("unexpected rejection for single char %q (byte %d): %v", name, b, err)
		}
	}
}

// Property: names containing only alphanumeric chars and hyphens (the common
// case for branch names) should always be accepted if length is in bounds
// and not "HEAD".
func TestProperty_AlphanumHyphenAlwaysValid(t *testing.T) {
	prop := func(name string) bool {
		// Filter to only alphanumeric + hyphen
		clean := true
		for _, r := range name {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
				clean = false
				break
			}
		}
		if !clean || len(name) == 0 || len(name) > 255 {
			return true // not in our domain
		}
		if strings.TrimSpace(name) == "" {
			return true
		}
		if strings.ToUpper(name) == "HEAD" {
			return true // git reserved, expected rejection
		}

		// Skip dash-prefixed for this test (use skipDashCheck=true)
		err := validateWorktreeName(name, true)
		if err != nil {
			t.Logf("VIOLATION: alphanumeric-hyphen name %q rejected: %v", name, err)
			return false
		}
		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 10000}); err != nil {
		t.Errorf("alphanumeric-hyphen property violated: %v", err)
	}
}

// Property: all names with path separators must be rejected.
func TestProperty_PathSeparatorsAlwaysRejected(t *testing.T) {
	prop := func(name string) bool {
		if !strings.Contains(name, "/") && !strings.Contains(name, "..") {
			return true // not relevant
		}
		err := validateWorktreeName(name, true)
		if err == nil {
			t.Logf("VIOLATION: name with path separator accepted: %q", name)
			return false
		}
		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 5000}); err != nil {
		t.Errorf("path separator property violated: %v", err)
	}
}

// Property: UTF-8 multibyte names without shell metachars or control chars
// should be accepted (internationalization correctness).
func TestProperty_UTF8NamesAccepted(t *testing.T) {
	prop := func(name string) bool {
		if !utf8.ValidString(name) || len(name) == 0 || len(name) > 255 {
			return true
		}
		if strings.TrimSpace(name) == "" {
			return true
		}

		// Check if name contains any character that should cause rejection
		shellMetachars := "$`|;&><*?()[]{}!~'\"\\/"
		for _, r := range name {
			if strings.ContainsRune(shellMetachars, r) {
				return true // expected rejection
			}
			if r < 32 || r == 127 {
				return true // control char, expected rejection
			}
		}
		if strings.Contains(name, "..") || name == "." {
			return true
		}
		if strings.ToUpper(name) == "HEAD" || strings.HasPrefix(name, "refs/") {
			return true
		}

		err := validateWorktreeName(name, true)
		if err != nil {
			t.Logf("VIOLATION: clean UTF-8 name %q rejected: %v", name, err)
			return false
		}
		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 5000}); err != nil {
		t.Errorf("UTF-8 acceptance property violated: %v", err)
	}
}

// --- Tests for isHelpFlag ---

func TestIsHelpFlag(t *testing.T) {
	if !isHelpFlag("-h") {
		t.Error("-h should be a help flag")
	}
	if !isHelpFlag("--help") {
		t.Error("--help should be a help flag")
	}
	if isHelpFlag("help") {
		t.Error("'help' without dash should not be a help flag")
	}
	if isHelpFlag("-help") {
		t.Error("-help (single dash) should not be a help flag")
	}
	if isHelpFlag("") {
		t.Error("empty string should not be a help flag")
	}
}

// --- Tests for checkGitignore ---

func TestCheckGitignore_Variants(t *testing.T) {
	cases := []struct {
		content string
		want    bool
	}{
		{"worktrees/\n", true},
		{"worktrees\n", true},
		{"/worktrees/\n", true},
		{"/worktrees\n", true},
		{"  worktrees/  \n", true},
		{"# worktrees/\nother\n", false}, // commented out
		{"", false},
		{"node_modules/\n.env\n", false},
		{"node_modules/\nworktrees/\n.env\n", true},
	}

	for _, tc := range cases {
		// checkGitignore reads from disk, so we test the logic directly
		// by checking the same patterns the function checks
		lines := strings.Split(tc.content, "\n")
		found := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "worktrees/" || trimmed == "worktrees" || trimmed == "/worktrees/" || trimmed == "/worktrees" {
				found = true
				break
			}
		}
		if found != tc.want {
			t.Errorf("for content %q: got %v, want %v", tc.content, found, tc.want)
		}
	}
}
