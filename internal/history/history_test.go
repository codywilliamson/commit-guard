package history

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
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
	gitRun(t, repo, "remote", "add", "origin", "https://example.invalid/project.git")
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

func TestNewBranchDirectDestinationUsesMatchingTrackingRefs(t *testing.T) {
	for _, destination := range []string{"https://example.invalid/project.git", "git@example.invalid:team/project.git", "../destination with spaces.git"} {
		t.Run(destination, func(t *testing.T) {
			repo := t.TempDir()
			gitRun(t, repo, "init", "-q")
			old := gitCommit(t, repo, "Old nonconforming history already on destination")
			head := gitCommit(t, repo, "feat: new work")
			gitRun(t, repo, "remote", "add", "origin", destination)
			gitRun(t, repo, "update-ref", "refs/remotes/origin/main", old)
			// A different remote knowing head must not hide work from this target.
			gitRun(t, repo, "remote", "add", "unrelated", "https://example.invalid/elsewhere.git")
			gitRun(t, repo, "update-ref", "refs/remotes/unrelated/main", head)
			actualDestination := destination
			if strings.HasPrefix(destination, "../") {
				actualDestination = filepath.Clean(filepath.Join(repo, destination))
			}
			input := fmt.Sprintf("HEAD %s refs/heads/new %s\n", head, zeroOID)
			got, err := Outgoing(repo, actualDestination, strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].SHA != head {
				t.Fatalf("want only new commit; got %#v", got)
			}
		})
	}
}

func TestUnknownDirectDestinationDoesNotExcludeUnrelatedHistory(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	head := gitCommit(t, repo, "feat: unpushed work")
	gitRun(t, repo, "remote", "add", "origin", "https://example.invalid/known.git")
	gitRun(t, repo, "update-ref", "refs/remotes/origin/main", head)
	input := fmt.Sprintf("HEAD %s refs/heads/new %s\n", head, zeroOID)
	_, err := Outgoing(repo, "https://example.invalid/unknown.git", strings.NewReader(input))
	if err == nil || !strings.Contains(err.Error(), "configured remote") {
		t.Fatalf("expected offline baseline diagnostic, got %v", err)
	}
}

func TestExistingBranchDirectPathNeedsNoTrackingBaseline(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	old := gitCommit(t, repo, "Old history")
	head := gitCommit(t, repo, "feat: new work")
	input := fmt.Sprintf("HEAD %s refs/heads/main %s\n", head, old)
	got, err := Outgoing(repo, filepath.Join(t.TempDir(), "destination with spaces.git"), strings.NewReader(input))
	if err != nil || len(got) != 1 || got[0].SHA != head {
		t.Fatalf("explicit range failed: %#v %v", got, err)
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
