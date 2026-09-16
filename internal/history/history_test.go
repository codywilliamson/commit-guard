package history

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestRangeIncludesFullAncestryWhenFromEmpty(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	c1 := gitCommit(t, repo, "feat: first")
	gitCommit(t, repo, "fix: second")
	c3 := gitCommit(t, repo, "docs: third")
	all, err := Range(repo, "", c3)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].SHA != c3 || !strings.Contains(all[2].Message, "feat: first") {
		t.Fatalf("full ancestry = %#v", all)
	}
	delta, err := Range(repo, c1, c3)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta) != 2 {
		t.Fatalf("range delta has %d commits, want 2", len(delta))
	}
	if _, err := Range(repo, "not-a-sha", c3); err == nil {
		t.Error("malformed from unexpectedly accepted")
	}
	if _, err := Range(repo, strings.Repeat("f", 40), c3); err == nil {
		t.Error("missing from object unexpectedly accepted")
	}
}

func TestOutgoingDeduplicatesRefsAndSkipsTagsAndDeletes(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	c1 := gitCommit(t, repo, "feat: first")
	gitCommit(t, repo, "fix: second")
	c3 := gitCommit(t, repo, "docs: third")
	gitRun(t, repo, "update-ref", "refs/remotes/origin/main", c1)
	input := fmt.Sprintf("HEAD %s refs/heads/main %s\n", c3, c1)
	input += fmt.Sprintf("refs/heads/new %s refs/heads/new %s\n", c3, zeroOID)
	input += "refs/tags/v1 invalid refs/tags/v1 invalid\n"
	input += fmt.Sprintf("(delete) %s refs/heads/deleted %s\n", zeroOID, c1)
	got, err := Outgoing(repo, "origin", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("outgoing has %d commits, want 2: %#v", len(got), got)
	}
	seen := map[string]bool{}
	for _, commit := range got {
		seen[commit.SHA] = true
	}
	if !seen[c3] {
		t.Errorf("outgoing omitted %s", c3)
	}
	if _, err := Outgoing(repo, "origin", strings.NewReader(fmt.Sprintf("refs/heads/main %s refs/heads/main %s\n", c3, strings.Repeat("f", 40)))); err == nil {
		t.Error("missing old object unexpectedly accepted")
	}
}

func gitCommit(t *testing.T, repo, message string) string {
	t.Helper()
	gitRun(t, repo, "-c", "user.name=commit-guard test", "-c", "user.email=commit-guard@example.invalid", "commit", "--allow-empty", "-q", "-m", message)
	return strings.TrimSpace(gitRun(t, repo, "rev-parse", "HEAD"))
}

func gitRun(t *testing.T, repo string, args ...string) string {
	t.Helper()
	all := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", all...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(bytes.TrimSpace(out))
}
