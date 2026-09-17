package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

const Repository = "codywilliamson/commit-guard"

var releaseRef = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`)
var shaRef = regexp.MustCompile(`^[0-9a-f]{40}$`)

func ValidRef(ref string) bool { return releaseRef.MatchString(ref) || shaRef.MatchString(ref) }

func Git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func Root() (string, error) { return Git("", "rev-parse", "--show-toplevel") }

func Home(root string) (string, error) {
	// Normal repositories and linked worktrees expose enough information on
	// disk to avoid spawning Git for every native hook invocation.
	if os.Getenv("GIT_COMMON_DIR") == "" {
		gitDir := os.Getenv("GIT_DIR")
		if gitDir == "" {
			gitDir = ".git"
		}
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(root, gitDir)
		}
		if info, err := os.Stat(gitDir); err == nil {
			if !info.IsDir() {
				if data, err := os.ReadFile(gitDir); err == nil && strings.HasPrefix(string(data), "gitdir: ") {
					resolved := strings.TrimSpace(strings.TrimPrefix(string(data), "gitdir: "))
					if !filepath.IsAbs(resolved) {
						resolved = filepath.Join(filepath.Dir(gitDir), resolved)
					}
					gitDir = resolved
				}
			}
			if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
				if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
					common := strings.TrimSpace(string(data))
					if !filepath.IsAbs(common) {
						common = filepath.Join(gitDir, common)
					}
					return filepath.Join(common, "commit-guard"), nil
				} else if os.IsNotExist(err) {
					return filepath.Join(gitDir, "commit-guard"), nil
				}
			}
		}
	}
	common, err := Git(root, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	return filepath.Join(common, "commit-guard"), nil
}

// RequiredRef reads the same action reference Dependabot changes. There is no
// independent local version pin that can drift from the workflow.
func RequiredRef(root string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(root, ".github", "workflows"))
	if err != nil {
		return "", fmt.Errorf("find commit-guard workflow: %w", err)
	}
	refs := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !(strings.HasSuffix(entry.Name(), ".yml") || strings.HasSuffix(entry.Name(), ".yaml")) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, ".github", "workflows", entry.Name()))
		if err != nil {
			return "", err
		}
		var doc map[string]any
		if err := yaml.Unmarshal(content, &doc); err != nil {
			return "", fmt.Errorf("parse workflow %s: %w", entry.Name(), err)
		}
		var walk func(any)
		walk = func(value any) {
			switch v := value.(type) {
			case map[string]any:
				if use, ok := v["uses"].(string); ok {
					for _, prefix := range []string{Repository + "@", Repository + "/.github/workflows/commitlint.yml@"} {
						if strings.HasPrefix(use, prefix) {
							refs[strings.TrimPrefix(use, prefix)] = true
						}
					}
				}
				for _, child := range v {
					walk(child)
				}
			case []any:
				for _, child := range v {
					walk(child)
				}
			}
		}
		walk(doc)
	}
	if len(refs) != 1 {
		return "", fmt.Errorf("expected one commit-guard release reference across workflows; found %d. Pin all uses to the same release", len(refs))
	}
	for ref := range refs {
		if !ValidRef(ref) {
			return "", fmt.Errorf("pin commit-guard to an exact release (v0.3.1) or full commit SHA; found %q", ref)
		}
		return ref, nil
	}
	panic("unreachable")
}

type State struct {
	Ref     string `json:"ref"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

func readState(home, ref string) (State, error) {
	var state State
	data, err := os.ReadFile(filepath.Join(home, "versions", ref, "state.json"))
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

func CheckVersion(root, runningVersion string) error {
	home, err := Home(root)
	if err != nil {
		return err
	}
	return CheckVersionAt(root, home, runningVersion)
}

// CheckVersionAt reuses the directory already resolved by the hook launcher.
func CheckVersionAt(root, home, runningVersion string) error {
	ref, err := RequiredRef(root)
	if err != nil {
		return err
	}
	active, err := os.ReadFile(filepath.Join(home, "active"))
	if err != nil {
		return fmt.Errorf("commit-guard setup is missing; run the installer")
	}
	installed := strings.TrimSpace(string(active))
	if installed != ref {
		return fmt.Errorf("commit-guard setup is outdated: installed %s; repository requires %s. Run: git commit-guard sync", installed, ref)
	}
	state, err := readState(home, installed)
	if err != nil || state.Ref != ref || state.Version != runningVersion {
		return fmt.Errorf("commit-guard installation does not match %s; run: git commit-guard sync", ref)
	}
	return nil
}

func Status(root, runningVersion string) error {
	ref, err := RequiredRef(root)
	if err != nil {
		return err
	}
	fmt.Printf("Repository requires: %s\nRunning checker: %s\n", ref, runningVersion)
	if err := CheckVersion(root, runningVersion); err != nil {
		return err
	}
	hooks, err := Git(root, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(hooks) {
		hooks = filepath.Join(root, hooks)
	}
	if filepath.Base(hooks) == "_" && strings.Contains(filepath.ToSlash(hooks), ".husky/") {
		hooks = filepath.Dir(hooks)
	}
	for _, name := range []string{"commit-msg", "pre-push"} {
		data, _, err := readHook(hooks, name)
		home, _ := Home(root)
		if err != nil || (!knownNative(home, data) && !strings.Contains(string(data), beginMarker)) {
			return fmt.Errorf("%s hook is inactive; re-run the installer", name)
		}
	}
	fmt.Println("Local checker matches and both hooks are installed. Sync command: git commit-guard sync")
	return nil
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
