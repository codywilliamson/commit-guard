package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckCLI(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		code        int
	}{
		{"ordinary", `[{"sha":"abc","message":"feat: Add Something."}]`, 0},
		{"long", `[{"sha":"abc","message":"feat: ` + strings.Repeat("long ", 80) + `"}]`, 0},
		{"invalid", `[{"sha":"abc","message":"Initial plan"}]`, 1},
		{"bad batch", `[] {}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, err bytes.Buffer
			code := run([]string{"check", "--batch", "--json", "--config", filepath.Join(t.TempDir(), "absent.json")}, strings.NewReader(tc.input), &out, &err)
			if code != tc.code {
				t.Fatalf("exit %d want %d: %s", code, tc.code, err.String())
			}
			if code < 2 {
				var report map[string]any
				if json.Unmarshal(out.Bytes(), &report) != nil {
					t.Fatalf("invalid report: %s", out.String())
				}
			}
		})
	}
}

func command(t *testing.T, dir, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustCommand(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, err := command(t, dir, name, args...)
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(out)
}

func buildCLI(t *testing.T) string {
	t.Helper()
	name := "commit-guard"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	mustCommand(t, ".", "go", "build", "-o", bin, ".")
	return bin
}

func makeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustCommand(t, root, "git", "init", "-q", "--initial-branch=main")
	mustCommand(t, root, "git", "config", "user.name", "Commit Guard Test")
	mustCommand(t, root, "git", "config", "user.email", "test@example.invalid")
	mustCommand(t, root, "git", "config", "commit.gpgsign", "false")
	mustCommand(t, root, "git", "config", "core.autocrlf", "false")
	mustCommand(t, root, "git", "commit", "--allow-empty", "-qm", "feat: Seed Repo.")
	return root
}

func TestInstalledHooksBlockBeforeCommitAndPush(t *testing.T) {
	bin := buildCLI(t)
	root := makeRepo(t)
	hooks := filepath.Join(root, ".githooks")
	mustCommand(t, root, "git", "config", "core.hooksPath", ".githooks")
	if err := os.MkdirAll(hooks, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "commit-msg"), []byte("#!/bin/sh\nprintf 'called\\n' >> existing-hook.log\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\ncat > existing-push-input\n"), 0755); err != nil {
		t.Fatal(err)
	}
	mustCommand(t, root, bin, "install")
	mustCommand(t, root, bin, "status")
	mustCommand(t, root, "git", "commit-guard", "status")
	mustCommand(t, root, "git", "commit-guard", "sync")
	if out, err := command(t, root, "git", "commit", "--allow-empty", "-qm", "Initial plan"); err == nil || !strings.Contains(out, "commit-guard") {
		t.Fatalf("invalid commit accepted or unrelated failure: %v %s", err, out)
	}
	mustCommand(t, root, "git", "commit", "--allow-empty", "-qm", "feat: Add Something. "+strings.Repeat("long ", 40))
	if _, err := os.Stat(filepath.Join(root, "existing-hook.log")); err != nil {
		t.Fatal("existing commit hook did not run")
	}
	remote := t.TempDir()
	mustCommand(t, remote, "git", "init", "--bare", "-q")
	mustCommand(t, root, "git", "remote", "add", "origin", remote)
	mustCommand(t, root, "git", "push", "-q", "origin", "HEAD:main")
	input, err := os.ReadFile(filepath.Join(root, "existing-push-input"))
	if err != nil || !bytes.Contains(input, []byte("refs/heads/main")) {
		t.Fatalf("existing push hook lost stdin: %v %q", err, input)
	}
	before := mustCommand(t, root, "git", "rev-parse", "HEAD")
	mustCommand(t, root, "git", "-c", "core.hooksPath=", "commit", "--allow-empty", "-qm", "Bad commit bypassed hook")
	if out, err := command(t, root, "git", "push", "-q", "origin", "HEAD:main"); err == nil || !strings.Contains(out, "commit-guard") {
		t.Fatalf("invalid outgoing commit accepted: %v %s", err, out)
	}
	if got := mustCommand(t, remote, "git", "rev-parse", "refs/heads/main"); got != before {
		t.Fatal("remote changed despite pre-push rejection")
	}
	mustCommand(t, root, bin, "uninstall")
	data, _ := os.ReadFile(filepath.Join(hooks, "commit-msg"))
	if bytes.Contains(data, []byte("# >>> commit-guard >>>")) || !bytes.Contains(data, []byte("existing-hook.log")) {
		t.Fatalf("uninstall damaged original hook: %s", data)
	}
}

func TestOutdatedInstallationFailsOffline(t *testing.T) {
	bin := buildCLI(t)
	root := makeRepo(t)
	mustCommand(t, root, bin, "install")
	wf := filepath.Join(root, ".github", "workflows", "commitlint.yml")
	data, err := os.ReadFile(wf)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("commit-guard@v0.3.0"), []byte("commit-guard@v9.9.9"))
	if err := os.WriteFile(wf, data, 0644); err != nil {
		t.Fatal(err)
	}
	out, err := command(t, root, "git", "commit", "--allow-empty", "-qm", "feat: Fine message")
	if err == nil || !strings.Contains(out, "outdated") || !strings.Contains(out, "sync") {
		t.Fatalf("missing repair notice: %v %s", err, out)
	}
	if strings.Contains(out, "https:") {
		t.Fatalf("hook attempted network instead of local notice: %s", out)
	}
}

func TestDirectDestinationPushDoesNotRelintRemoteHistory(t *testing.T) {
	bin := buildCLI(t)
	root := makeRepo(t)
	remote := filepath.Join(t.TempDir(), "destination with spaces.git")
	mustCommand(t, root, "git", "init", "--bare", "-q", remote)
	mustCommand(t, root, "git", "remote", "add", "origin", remote)
	// Seed historical content before this repository adopts commit-guard.
	mustCommand(t, root, "git", "commit", "--allow-empty", "-qm", "Historical nonconforming subject")
	mustCommand(t, root, "git", "push", "-q", "origin", "HEAD:main")
	mustCommand(t, root, bin, "install")
	mustCommand(t, root, "git", "commit", "--allow-empty", "-qm", "feat: new branch work")
	want := mustCommand(t, root, "git", "rev-parse", "HEAD")
	// A path, rather than the configured name, becomes the hook's first arg.
	mustCommand(t, root, "git", "push", "-q", remote, "HEAD:new-branch")
	if got := mustCommand(t, remote, "git", "rev-parse", "refs/heads/new-branch"); got != want {
		t.Fatalf("direct push did not reach expected commit: %s", got)
	}
}
