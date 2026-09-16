package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/codywilliamson/commit-guard/internal/checker"
	"go.yaml.in/yaml/v3"
)

const beginMarker = "# >>> commit-guard >>>"
const endMarker = "# <<< commit-guard <<<"

type Options struct {
	Ref       string
	Mode      string
	CIOnly    bool
	NoUpdates bool
}

func workflow(ref, mode string) string {
	return fmt.Sprintf(`# Managed by commit-guard. The action reference also pins local validation.
name: Commit Guard
on:
  pull_request:
    types: [opened, synchronize, reopened, edited]
  push:
    branches: [main, master]
  merge_group:
    types: [checks_requested]
permissions:
  contents: read
  pull-requests: read
jobs:
  commit-guard:
    name: Commit Guard
    runs-on: ubuntu-latest
    timeout-minutes: 5
    concurrency:
      group: commit-guard-${{ github.workflow }}-${{ github.event.pull_request.number || github.run_id }}
      cancel-in-progress: ${{ github.event_name == 'pull_request' }}
    steps:
      - uses: %s@%s
        with:
          mode: %s
`, Repository, ref, mode)
}

func Install(root, version string, opts Options) error {
	if opts.Ref == "" {
		opts.Ref = "v" + version
	}
	if !ValidRef(opts.Ref) {
		return fmt.Errorf("invalid release reference %q", opts.Ref)
	}
	if opts.Mode == "" || opts.Mode == "smart" {
		opts.Mode = "commits"
	}
	if opts.Mode != "commits" && opts.Mode != "title" {
		return fmt.Errorf("mode must be commits or title")
	}
	// Validate existing policy and hook integration before changing any files.
	if _, err := checker.LoadConfig(filepath.Join(root, ".commit-guard.json")); err != nil {
		return err
	}
	if !opts.CIOnly {
		alias, _ := Git(root, "config", "--get", "alias.commit-guard")
		if alias != "" && !strings.Contains(alias, "# commit-guard management") {
			return fmt.Errorf("git alias commit-guard already exists; preserve or rename that alias before installing")
		}
		dir, err := hooksDir(root)
		if err != nil {
			return err
		}
		for _, name := range []string{"commit-msg", "pre-push"} {
			old, _, err := readHook(dir, name)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			home, err := Home(root)
			if err != nil {
				return err
			}
			if knownNative(home, old) {
				continue
			}
			if _, err := hookContent(string(old), name); err != nil {
				return err
			}
		}
	}
	wf := filepath.Join(root, ".github", "workflows", "commitlint.yml")
	if old, err := os.ReadFile(wf); err == nil {
		if !strings.Contains(string(old), Repository) {
			return fmt.Errorf("%s already exists and is not a commit-guard workflow; preserve it and install commit-guard in a separate workflow", wf)
		}
		if strings.Contains(string(old), Repository+"/.github/workflows/commitlint.yml@v0.2.") || strings.Contains(string(old), Repository+"/.github/workflows/commitlint.yml@v0.1.") {
			// Migrating the old generated workflow is explicit installer work. Keep
			// a backup; existing v0.3+ pins continue to belong to Dependabot.
			home, err := Home(root)
			if err != nil {
				return err
			}
			backup := filepath.Join(home, "backups", "commitlint.yml")
			if _, err := os.Stat(backup); os.IsNotExist(err) {
				if err := atomicWrite(backup, old, 0644); err != nil {
					return err
				}
			}
			migrated, err := migrateLegacyWorkflow(old, opts.Ref)
			if err != nil {
				return err
			}
			if err := atomicWrite(wf, migrated, 0644); err != nil {
				return err
			}
		} else {
			// An existing pin belongs to the repository. Installation activates it;
			// releases are proposed by Dependabot, not changed behind the user's back.
			ref, err := RequiredRef(root)
			if err != nil {
				return err
			}
			opts.Ref = ref
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		if err := atomicWrite(wf, []byte(workflow(opts.Ref, opts.Mode)), 0644); err != nil {
			return err
		}
	}
	config := filepath.Join(root, ".commit-guard.json")
	if info, err := os.Stat(config); os.IsNotExist(err) {
		if err := atomicWrite(config, []byte("{\n  \"maxHeaderLength\": 0\n}\n"), 0644); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf(".commit-guard.json must be a regular file")
	}
	if !opts.NoUpdates {
		if err := configureUpdates(root); err != nil {
			return err
		}
	}
	if opts.CIOnly {
		fmt.Println("Commit Guard workflow and update configuration installed.")
		return nil
	}
	if err := Sync(root, version, true); err != nil {
		return err
	}
	if err := installHooks(root); err != nil {
		return err
	}
	alias := fmt.Sprintf(`!f() { # commit-guard management
cg_home="$(git rev-parse --git-common-dir)/commit-guard"
cg_ref="$(cat "$cg_home/active")"
case "$cg_ref" in *[!a-zA-Z0-9.-]*|'') echo "Invalid commit-guard installation" >&2; return 2;; esac
"$cg_home/versions/$cg_ref/%s" "$@"
}; f`, BinaryName())
	if _, err := Git(root, "config", "--local", "alias.commit-guard", alias); err != nil {
		return err
	}
	fmt.Println("Commit Guard installed: commit-msg and pre-push hooks with shared policy.")
	fmt.Println("Manage this checkout with: git commit-guard status | sync | uninstall")
	return nil
}

func migrateLegacyWorkflow(content []byte, ref string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, err
	}
	var walk func(*yaml.Node)
	var migrationError error
	walk = func(node *yaml.Node) {
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				if key.Value == "uses" && strings.HasPrefix(value.Value, Repository+"/.github/workflows/commitlint.yml@") {
					for j := 0; j+1 < len(node.Content); j += 2 {
						if node.Content[j].Value != "with" {
							continue
						}
						with := node.Content[j+1]
						for k := 0; k+1 < len(with.Content); k += 2 {
							name, val := with.Content[k].Value, with.Content[k+1].Value
							unsupported := (name == "config" && val != "conventional") || ((name == "ignore-bot-commits" || name == "ignore-merge-commits") && val != "true") || (name == "ignore-message-patterns" && val != "")
							if unsupported {
								migrationError = fmt.Errorf("legacy option %s=%q needs migration to .commit-guard.json before upgrading", name, val)
							}
						}
					}
					value.Value = Repository + "/.github/workflows/commitlint.yml@" + ref
				}
			}
		}
		for _, child := range node.Content {
			walk(child)
		}
	}
	walk(&doc)
	if migrationError != nil {
		return nil, migrationError
	}
	return yaml.Marshal(&doc)
}

func Sync(root, runningVersion string, allowSelf bool) error {
	ref, err := RequiredRef(root)
	if err != nil {
		return err
	}
	home, err := Home(root)
	if err != nil {
		return err
	}
	target := filepath.Join(home, "versions", ref, BinaryName())
	state, cachedErr := readState(home, ref)
	if cachedErr == nil && state.Ref == ref {
		if data, err := os.ReadFile(target); err == nil {
			digest := sha256.Sum256(data)
			if hex.EncodeToString(digest[:]) == state.SHA256 {
				if err := atomicWrite(filepath.Join(home, "active"), []byte(ref+"\n"), 0644); err != nil {
					return err
				}
				if err := installHooks(root); err != nil {
					return err
				}
				fmt.Printf("Activated cached commit-guard %s (%s).\n", state.Version, ref)
				return nil
			}
		}
	}
	var data []byte
	var digest, version string
	if allowSelf && ref == "v"+runningVersion {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		data, err = os.ReadFile(exe)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		digest = hex.EncodeToString(sum[:])
		version = runningVersion
	} else {
		version, err = ResolveVersion(ref)
		if err != nil {
			return err
		}
		data, digest, err = Download(version)
		if err != nil {
			return err
		}
	}
	if err := atomicWrite(target, data, 0755); err != nil {
		return err
	}
	state = State{Ref: ref, Version: version, SHA256: digest}
	encoded, _ := json.MarshalIndent(state, "", "  ")
	if err := atomicWrite(filepath.Join(home, "versions", ref, "state.json"), encoded, 0644); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(home, "active"), []byte(ref+"\n"), 0644); err != nil {
		return err
	}
	fmt.Printf("Activated commit-guard %s (%s).\n", version, ref)
	return installHooks(root)
}

func removeBlock(content string) (string, error) {
	start, end := -1, -1
	offset := 0
	for _, line := range strings.SplitAfter(content, "\n") {
		value := strings.TrimSuffix(line, "\n")
		if value == beginMarker {
			if start >= 0 {
				return "", fmt.Errorf("multiple commit-guard hook blocks")
			}
			start = offset
		}
		if value == endMarker {
			if end >= 0 {
				return "", fmt.Errorf("multiple commit-guard hook blocks")
			}
			end = offset
		}
		offset += len(line)
	}
	if start < 0 && end < 0 {
		return content, nil
	}
	if start < 0 || end < start {
		return "", fmt.Errorf("incomplete commit-guard hook block; repair the hook before installing")
	}
	end += len(endMarker)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return content[:start] + content[end:], nil
}

func hookContent(existing, kind string) (string, error) {
	if !utf8.ValidString(existing) || strings.ContainsRune(existing, 0) {
		return "", fmt.Errorf("existing %s hook is a binary; integrate commit-guard manually", kind)
	}
	existing = strings.ReplaceAll(existing, "\r\n", "\n")
	var err error
	existing, err = removeBlock(existing)
	if err != nil {
		return "", err
	}
	// Recognize the exact old project's generated native validator by its
	// identifying error and regex. Back it up before replacing during install.
	legacyHash := sha256.Sum256([]byte(strings.TrimSpace(existing)))
	if hex.EncodeToString(legacyHash[:]) == "dfb3008027370e01b771ccdb125b5c4feff7f4923938460bb2b49bfd0aa77410" {
		existing = ""
	}
	header := "#!/usr/bin/env sh\n"
	if strings.HasPrefix(existing, "#!") {
		line, rest, found := strings.Cut(existing, "\n")
		if !strings.Contains(line, "sh") {
			return "", fmt.Errorf("existing %s hook is not a shell hook; integrate the checker manually", kind)
		}
		header = line + "\n"
		existing = rest
		if !found {
			existing = ""
		}
	}
	if kind == "commit-msg" {
		for _, legacy := range []string{`npx --no -- commitlint --edit "$1"`, `pnpm exec commitlint --edit "$1"`, `yarn commitlint --edit "$1"`} {
			if strings.TrimSpace(existing) == legacy {
				existing = ""
			}
		}
	}
	if strings.TrimSpace(existing) != "" && !strings.HasPrefix(header, "#!") {
		return "", fmt.Errorf("unknown hook format")
	}
	bin := "commit-guard"
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	invocation := fmt.Sprintf(`COMMIT_GUARD_HOME="$cg_home" "$cg_home/versions/$cg_ref/%s" hook %s "$@" || exit $?`, bin, kind)
	if kind == "pre-push" {
		// Preserve Git's ref stream for an existing pre-push hook after ours.
		invocation = fmt.Sprintf(`cg_push_input="$(cat)"
printf '%%s\n' "$cg_push_input" | COMMIT_GUARD_HOME="$cg_home" "$cg_home/versions/$cg_ref/%s" hook pre-push "$@" || exit $?
exec 0<<COMMIT_GUARD_PUSH_REFS
$cg_push_input
COMMIT_GUARD_PUSH_REFS
unset cg_push_input`, bin)
	}
	block := fmt.Sprintf(`%s
cg_home="$(git rev-parse --git-common-dir)/commit-guard"
if [ ! -f "$cg_home/active" ]; then
  echo "commit-guard setup is missing; re-run the installer." >&2
  exit 2
fi
cg_ref="$(cat "$cg_home/active")"
case "$cg_ref" in *[!a-zA-Z0-9.-]*|'') echo "Invalid commit-guard installation." >&2; exit 2;; esac
%s
unset cg_home cg_ref
%s
`, beginMarker, invocation, endMarker)
	return header + block + existing, nil
}

func hooksDir(root string) (string, error) {
	path, _ := Git(root, "config", "--get", "core.hooksPath")
	if path == "" {
		defaultDir, err := Git(root, "rev-parse", "--git-path", "hooks")
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(defaultDir) {
			defaultDir = filepath.Join(root, defaultDir)
		}
		return defaultDir, nil
	}
	resolved, err := Git(root, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return "", err
	}
	path = resolved
	// Husky's internal wrappers delegate to the parent directory's hooks.
	if filepath.Base(filepath.FromSlash(path)) == "_" && strings.Contains(filepath.ToSlash(path), ".husky/") {
		path = filepath.Dir(filepath.FromSlash(path))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("existing hooks path %s is outside this repository; integrate commit-guard manually to preserve shared hooks", path)
	}
	return path, nil
}

func installHooks(root string) error {
	dir, err := hooksDir(root)
	if err != nil {
		return err
	}
	home, err := Home(root)
	if err != nil {
		return err
	}
	active, err := os.ReadFile(filepath.Join(home, "active"))
	if err != nil {
		return err
	}
	ref := strings.TrimSpace(string(active))
	if !ValidRef(ref) {
		return fmt.Errorf("invalid installed reference")
	}
	binary, err := os.ReadFile(filepath.Join(home, "versions", ref, BinaryName()))
	if err != nil {
		return err
	}
	privateHooks := filepath.Clean(dir) == filepath.Join(filepath.Dir(home), "hooks")
	contents := map[string][]byte{}
	for _, name := range []string{"commit-msg", "pre-push"} {
		old, existingPath, err := readHook(dir, name)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if privateHooks && (len(old) == 0 || knownNative(home, old) || strings.TrimSpace(string(old)) == "#!/usr/bin/env sh") {
			fileName := name
			if runtime.GOOS == "windows" {
				fileName += ".exe"
			}
			if len(old) > 0 && existingPath != filepath.Join(dir, fileName) && knownNative(home, old) {
				if err := os.Remove(existingPath); err != nil {
					return err
				}
			}
			contents[fileName] = binary
			continue
		}
		content, err := hookContent(string(old), name)
		if err != nil {
			return err
		}
		contents[name] = []byte(content)
		backup := filepath.Join(home, "backups", name)
		if len(old) > 0 {
			if _, err := os.Stat(backup); os.IsNotExist(err) {
				if err := atomicWrite(backup, old, 0755); err != nil {
					return err
				}
			}
		}
	}
	for name := range contents {
		old, _ := os.ReadFile(filepath.Join(dir, name))
		if string(old) == string(contents[name]) {
			continue
		}
		if err := atomicWrite(filepath.Join(dir, name), contents[name], 0755); err != nil {
			return err
		}
	}
	current, _ := Git(root, "config", "--get", "core.hooksPath")
	if current == "" && filepath.Clean(dir) == filepath.Join(root, ".githooks") {
		_, err = Git(root, "config", "core.hooksPath", ".githooks")
	}
	return err
}

func readHook(dir, name string) ([]byte, string, error) {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && runtime.GOOS == "windows" {
		path += ".exe"
		data, err = os.ReadFile(path)
	}
	return data, path, err
}

func knownNative(home string, data []byte) bool {
	if len(data) < 1024 {
		return false
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	entries, err := os.ReadDir(filepath.Join(home, "versions"))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() || !ValidRef(entry.Name()) {
			continue
		}
		state, err := readState(home, entry.Name())
		if err == nil && state.SHA256 == digest {
			return true
		}
	}
	return false
}

func configureUpdates(root string) error {
	path := filepath.Join(root, ".github", "dependabot.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		other := filepath.Join(root, ".github", "dependabot.yaml")
		if _, err := os.Stat(other); err == nil {
			path = other
		}
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return atomicWrite(path, []byte("version: 2\nupdates:\n  - package-ecosystem: github-actions\n    directory: /\n    schedule:\n      interval: weekly\n"), 0644)
	}
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse existing Dependabot config: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("Dependabot config must be a mapping")
	}
	rootNode := doc.Content[0]
	var updates *yaml.Node
	for i := 0; i < len(rootNode.Content); i += 2 {
		if rootNode.Content[i].Value == "updates" {
			updates = rootNode.Content[i+1]
		}
	}
	if updates == nil {
		updates = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		rootNode.Content = append(rootNode.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "updates"}, updates)
	}
	if updates.Kind != yaml.SequenceNode {
		return fmt.Errorf("Dependabot updates must be a list")
	}
	for _, entry := range updates.Content {
		var ecosystem, directory, target string
		var directories []string
		for i := 0; i+1 < len(entry.Content); i += 2 {
			switch entry.Content[i].Value {
			case "package-ecosystem":
				ecosystem = entry.Content[i+1].Value
			case "directory":
				directory = entry.Content[i+1].Value
			case "target-branch":
				target = entry.Content[i+1].Value
			case "directories":
				for _, n := range entry.Content[i+1].Content {
					directories = append(directories, n.Value)
				}
			}
		}
		coversRoot := directory == "/"
		for _, dir := range directories {
			if dir == "/" {
				coversRoot = true
			}
		}
		if ecosystem == "github-actions" && coversRoot && target == "" {
			fmt.Println("Existing root GitHub Actions update schedule preserved.")
			return nil
		}
	}
	var addition yaml.Node
	if err := yaml.Unmarshal([]byte("package-ecosystem: github-actions\ndirectory: /\nschedule:\n  interval: weekly\n"), &addition); err != nil {
		return err
	}
	updates.Content = append(updates.Content, addition.Content[0])
	encoded, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	return atomicWrite(path, encoded, 0644)
}

func Uninstall(root string) error {
	dir, err := hooksDir(root)
	if err != nil {
		return err
	}
	home, err := Home(root)
	if err != nil {
		return err
	}
	for _, name := range []string{"commit-msg", "pre-push"} {
		data, path, err := readHook(dir, name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if knownNative(home, data) {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		clean, err := removeBlock(string(data))
		if err != nil {
			return err
		}
		if err := atomicWrite(path, []byte(clean), 0755); err != nil {
			return err
		}
	}
	fmt.Println("Removed commit-guard hooks and managed blocks. Repository workflows, policy, and other hooks are preserved.")
	return nil
}
