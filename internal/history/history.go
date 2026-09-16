// Package history reads commit messages from git without invoking a shell.
// Pre-push collection intentionally handles branch refs only; tag updates are
// ignored because commit-guard validates branch history.
package history

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"

	"github.com/codywilliamson/commit-guard/internal/checker"
)

const zeroOID = "0000000000000000000000000000000000000000"

var oidPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// Range returns commits reachable from to and not reachable from from. When
// from is empty, all of to's ancestry is returned, including to itself.
// Arguments are object IDs (full SHA-1 values); refs and shell expressions
// are rejected before git is run.
func Range(repo, from, to string) ([]checker.Commit, error) {
	if err := validateOID(to, "to"); err != nil {
		return nil, err
	}
	if from != "" {
		if err := validateOID(from, "from"); err != nil {
			return nil, err
		}
	}
	if from != "" {
		return logCommits(repo, from+".."+to)
	}
	return logCommits(repo, to)
}

// Outgoing parses git's pre-push input and returns the deduplicated commits
// introduced by branch updates. Delete lines and tag refs are ignored. A
// missing old object is reported by git; this function never fetches it.
func Outgoing(repo, remote string, stdin io.Reader) ([]checker.Commit, error) {
	if strings.TrimSpace(remote) == "" || strings.HasPrefix(remote, "-") || strings.ContainsAny(remote, "\x00\r\n\t ") {
		return nil, errors.New("invalid remote name")
	}
	if stdin == nil {
		return nil, errors.New("pre-push input is nil")
	}
	type update struct {
		old, local string
		newRef     bool
	}
	updates := make([]update, 0, 4)
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 4 {
			return nil, fmt.Errorf("invalid pre-push ref line %q", scanner.Text())
		}
		localOID, remoteRef, remoteOID := fields[1], fields[2], fields[3]
		// Git sends localRef as "(delete)" for deletion and permits symbolic
		// sources such as HEAD:main. The destination ref determines whether
		// this is a branch update; localRef is deliberately not constrained.
		if !strings.HasPrefix(remoteRef, "refs/heads/") {
			continue
		}
		if err := validateOID(localOID, "local object"); err != nil {
			return nil, err
		}
		if err := validateOID(remoteOID, "remote object"); err != nil {
			return nil, err
		}
		if localOID == zeroOID {
			continue // branch deletion
		}
		updates = append(updates, update{old: remoteOID, local: localOID, newRef: remoteOID == zeroOID})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read pre-push input: %w", err)
	}

	if len(updates) == 0 {
		return []checker.Commit{}, nil
	}
	commits := make([]checker.Commit, 0)
	seen := make(map[string]struct{})
	for _, u := range updates {
		var revisions []string
		if u.newRef {
			revisions = []string{u.local, "--not", "--remotes=" + remote}
		} else {
			revisions = []string{u.old + ".." + u.local}
		}
		got, err := logCommits(repo, revisions...)
		if err != nil {
			return nil, err
		}
		for _, commit := range got {
			if _, exists := seen[commit.SHA]; exists {
				continue
			}
			seen[commit.SHA] = struct{}{}
			commits = append(commits, commit)
		}
	}
	return commits, nil
}

func validateOID(value, name string) error {
	if !oidPattern.MatchString(value) {
		return fmt.Errorf("invalid %s object ID", name)
	}
	return nil
}

func logCommits(repo string, revisions ...string) ([]checker.Commit, error) {
	// Keep the NUL delimiters around both fields: commit bodies can contain
	// arbitrary newlines, so line-oriented parsing would be ambiguous.
	args := []string{"-C", repo, "--no-pager", "log", "--no-color", "--no-decorate", "--no-patch", "--format=%H%x00%B%x00"}
	args = append(args, revisions...)
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" {
				return nil, fmt.Errorf("git log: %s", stderr)
			}
		}
		return nil, fmt.Errorf("git log: %w", err)
	}
	return parseLog(out)
}

func parseLog(data []byte) ([]checker.Commit, error) {
	if len(data) == 0 {
		return []checker.Commit{}, nil
	}
	commits := make([]checker.Commit, 0)
	for pos := 0; pos < len(data); {
		shaEnd := bytes.IndexByte(data[pos:], 0)
		if shaEnd < 0 {
			return nil, errors.New("git log: malformed commit object ID")
		}
		sha := string(data[pos : pos+shaEnd])
		if !oidPattern.MatchString(sha) {
			return nil, errors.New("git log: malformed commit object ID")
		}
		msgStart := pos + shaEnd + 1
		msgEndRel := bytes.IndexByte(data[msgStart:], 0)
		if msgEndRel < 0 {
			return nil, errors.New("git log: malformed NUL output")
		}
		msgEnd := msgStart + msgEndRel
		commits = append(commits, checker.Commit{SHA: sha, Message: string(data[msgStart:msgEnd])})
		pos = msgEnd + 1
		// Git's pretty formats append a line terminator after each record, even
		// when the format itself already ended in a NUL.
		if pos < len(data) && data[pos] == '\n' {
			pos++
		}
	}
	return commits, nil
}
