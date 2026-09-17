package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequiredReferenceIsWorkflowPin(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, "guard.yml")
	for _, ref := range []string{"v0.3.0", strings.Repeat("a", 40)} {
		os.WriteFile(path, []byte(workflow(ref, "commits")), 0644)
		got, err := RequiredRef(root)
		if err != nil || got != ref {
			t.Fatalf("got %q: %v", got, err)
		}
	}
	os.WriteFile(filepath.Join(dir, "other.yml"), []byte(workflow("v0.4.0", "commits")), 0644)
	if _, err := RequiredRef(root); err == nil {
		t.Fatal("conflicting pins accepted")
	}
}

func TestDependabotPreservesExistingEcosystems(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".github"), 0755)
	path := filepath.Join(root, ".github", "dependabot.yml")
	os.WriteFile(path, []byte("# Existing config\nversion: 2\nupdates:\n  - package-ecosystem: npm\n    directory: /web\n    schedule:\n      interval: daily\n"), 0644)
	if err := configureUpdates(root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{"# Existing config", "npm", "/web", "daily", "github-actions", "weekly"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("lost %s: %s", want, data)
		}
	}
	before := string(data)
	if err := configureUpdates(root); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != before {
		t.Fatal("reinstall rewrote existing update schedule")
	}
}

func TestLegacyMigrationPreservesWorkflowJobs(t *testing.T) {
	data := []byte("name: Existing\non: [push]\njobs:\n  lint:\n    uses: codywilliamson/commit-guard/.github/workflows/commitlint.yml@v0.2.2\n    with:\n      pr-mode: title\n  unrelated:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo preserve-me\n")
	got, err := migrateLegacyWorkflow(data, "v0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@v0.3.0", "pr-mode: title", "preserve-me"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("lost workflow value %q: %s", want, got)
		}
	}
}

func TestNonShellHookIsNotOverwritten(t *testing.T) {
	if _, err := hookContent("#!/usr/bin/env python3\nprint('test')\n", "commit-msg"); err == nil {
		t.Fatal("accepted non-shell hook")
	}
}

func TestHookInstallIsIdempotent(t *testing.T) {
	original := "#!/bin/sh\necho custom\n"
	one, err := hookContent(original, "commit-msg")
	if err != nil {
		t.Fatal(err)
	}
	two, err := hookContent(one, "commit-msg")
	if err != nil {
		t.Fatal(err)
	}
	if one != two {
		t.Fatal("hook changes on repeated install")
	}
	clean, err := removeBlock(two)
	if err != nil || clean != original {
		t.Fatalf("original hook not recovered: %v %q", err, clean)
	}
}

func TestQuotedMarkerTextIsPreserved(t *testing.T) {
	text := "#!/bin/sh\necho '# >>> commit-guard >>>'\necho meaningful\necho '# <<< commit-guard <<<'\n"
	got, err := removeBlock(text)
	if err != nil || got != text {
		t.Fatalf("removed user code: %v %s", err, got)
	}
}

func TestLegacyHuskyRunnerIsReplaced(t *testing.T) {
	got, err := hookContent("#!/usr/bin/env sh\nnpx --no -- commitlint --edit \"$1\"\n", "commit-msg")
	if err != nil || strings.Contains(got, "npx") {
		t.Fatalf("kept divergent old validator: %v %s", err, got)
	}
}

func TestDependabotSubdirectoryDoesNotSuppressRootUpdates(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".github"), 0755)
	path := filepath.Join(root, ".github", "dependabot.yml")
	os.WriteFile(path, []byte("version: 2\nupdates:\n  - package-ecosystem: github-actions\n    directory: /web\n    schedule:\n      interval: daily\n"), 0644)
	if err := configureUpdates(root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), "github-actions") != 2 {
		t.Fatalf("root update entry not added: %s", data)
	}
}

func TestHomeResolvesLinkedWorktreeWithoutGit(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "main", ".git")
	worktree := filepath.Join(root, "linked")
	private := filepath.Join(common, "worktrees", "linked")
	os.MkdirAll(private, 0755)
	os.MkdirAll(worktree, 0755)
	os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+private+"\n"), 0644)
	os.WriteFile(filepath.Join(private, "commondir"), []byte("../..\n"), 0644)
	t.Setenv("GIT_DIR", "")
	t.Setenv("GIT_COMMON_DIR", "")
	got, err := Home(worktree)
	if err != nil || got != filepath.Join(common, "commit-guard") {
		t.Fatalf("wrong common dir %q: %v", got, err)
	}
}
