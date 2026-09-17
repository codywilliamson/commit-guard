// Package checker validates commit messages against a small Conventional
// Commits grammar.
package checker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"
)

// Config controls commit message validation. The field names correspond to
// the keys accepted in .commit-guard.json.
type Config struct {
	Types              []string `json:"types"`
	MaxHeaderLength    int      `json:"maxHeaderLength"`
	IgnoreMergeCommits bool     `json:"ignoreMergeCommits"`
	IgnoreFixupCommits bool     `json:"ignoreFixupCommits"`
}

var defaultTypes = []string{"build", "chore", "ci", "docs", "feat", "fix", "perf", "refactor", "revert", "style", "test"}

// DefaultConfig returns the standard validation configuration.
func DefaultConfig() Config {
	return Config{
		Types:              append([]string(nil), defaultTypes...),
		MaxHeaderLength:    0,
		IgnoreMergeCommits: true,
		IgnoreFixupCommits: true,
	}
}

// ParseConfig parses a JSON configuration. Unknown fields and any data after
// the JSON value are rejected. An empty document uses the defaults.
func ParseConfig(data []byte) (Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Config{}, errors.New("parse config: expected a JSON object")
	}

	cfg := DefaultConfig()
	// Pointers distinguish omitted booleans from explicitly supplied false
	// values, while retaining ordinary exported fields on Config.
	var raw struct {
		Types              *[]string `json:"types"`
		MaxHeaderLength    *int      `json:"maxHeaderLength"`
		IgnoreMergeCommits *bool     `json:"ignoreMergeCommits"`
		IgnoreFixupCommits *bool     `json:"ignoreFixupCommits"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if fields == nil {
		return Config{}, errors.New("parse config: expected a JSON object")
	}
	for name, value := range fields {
		switch name {
		case "types", "maxHeaderLength", "ignoreMergeCommits", "ignoreFixupCommits":
		default:
			return Config{}, fmt.Errorf("parse config: unknown field %q", name)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Config{}, fmt.Errorf("parse config: %s cannot be null", name)
		}
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, errors.New("parse config: trailing data")
		}
		return Config{}, fmt.Errorf("parse config: trailing data: %w", err)
	}
	if _, present := fields["types"]; present {
		if raw.Types == nil {
			cfg.Types = nil
		} else {
			cfg.Types = append([]string(nil), (*raw.Types)...)
		}
	}
	if raw.MaxHeaderLength != nil {
		cfg.MaxHeaderLength = *raw.MaxHeaderLength
	}
	if raw.IgnoreMergeCommits != nil {
		cfg.IgnoreMergeCommits = *raw.IgnoreMergeCommits
	}
	if raw.IgnoreFixupCommits != nil {
		cfg.IgnoreFixupCommits = *raw.IgnoreFixupCommits
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// LoadConfig loads path, returning defaults when the file does not exist.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DefaultConfig(), nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	return ParseConfig(data)
}

// Commit is the portion of a git commit needed by the checker.
type Commit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
}

// Result contains all validation errors for one commit. Errors is always
// non-nil, including for a valid commit.
type Result struct {
	SHA    string   `json:"sha"`
	Errors []string `json:"errors"`
}

// Check validates every commit and preserves input order.
func Check(commits []Commit, cfg Config) []Result {
	results := make([]Result, 0, len(commits))
	for _, commit := range commits {
		errList := Validate(commit.Message, cfg)
		if errList == nil {
			errList = []string{}
		}
		results = append(results, Result{SHA: commit.SHA, Errors: errList})
	}
	return results
}

// Validate returns all applicable errors for message. Only the first line is
// treated as the commit header; body lines do not affect its grammar.
func Validate(message string, cfg Config) []string {
	if err := validateConfig(cfg); err != nil {
		return []string{err.Error()}
	}
	header := message
	if i := strings.IndexByte(header, '\n'); i >= 0 {
		header = header[:i]
	}
	header = strings.TrimSuffix(header, "\r")
	if cfg.IgnoreMergeCommits && isMergeSubject(header) {
		return nil
	}
	if cfg.IgnoreFixupCommits && isFixupSubject(header) {
		return nil
	}
	if isRevertSubject(header) {
		return nil
	}

	errs := make([]string, 0, 2)
	if header == "" {
		errs = append(errs, "commit message header is empty")
		return errs
	}
	if cfg.MaxHeaderLength > 0 && utf8.RuneCountInString(header) > cfg.MaxHeaderLength {
		errs = append(errs, fmt.Sprintf("header exceeds maximum length of %d characters", cfg.MaxHeaderLength))
	}
	if err := validateHeader(header, cfg.Types); err != nil {
		errs = append(errs, err.Error())
	}
	return errs
}

func validateConfig(cfg Config) error {
	if cfg.MaxHeaderLength < 0 {
		return errors.New("maxHeaderLength must be zero or greater")
	}
	if len(cfg.Types) == 0 {
		return errors.New("types must contain at least one type")
	}
	seen := make(map[string]struct{}, len(cfg.Types))
	for _, typ := range cfg.Types {
		if typ == "" || strings.TrimSpace(typ) != typ {
			return errors.New("types must not contain empty or whitespace-padded values")
		}
		if strings.IndexFunc(typ, func(r rune) bool {
			return r == ':' || r == '(' || r == ')' || r == '!' || r == '\r' || r == '\n' || r == ' ' || r == '\t'
		}) >= 0 {
			return fmt.Errorf("invalid commit type %q", typ)
		}
		key := strings.ToLower(typ)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate commit type %q", typ)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateHeader(header string, types []string) error {
	colon := strings.Index(header, ": ")
	if colon < 1 {
		return errors.New("header must match type(scope)!: subject")
	}
	prefix, subject := header[:colon], header[colon+2:]
	if strings.TrimSpace(subject) == "" {
		return errors.New("commit subject must not be empty")
	}
	if strings.ContainsAny(prefix, "\r\n") {
		return errors.New("header must match type(scope)!: subject")
	}

	if bang := strings.HasSuffix(prefix, "!"); bang {
		prefix = strings.TrimSuffix(prefix, "!")
	}
	typ := prefix
	if open := strings.IndexByte(prefix, '('); open >= 0 {
		if !strings.HasSuffix(prefix, ")") || strings.IndexByte(prefix[open+1:], '(') >= 0 || strings.Contains(prefix[open+1:len(prefix)-1], ")") {
			return errors.New("scope must be non-empty and may not contain nested parentheses")
		}
		typ = prefix[:open]
		scope := prefix[open+1 : len(prefix)-1]
		if strings.TrimSpace(scope) == "" {
			return errors.New("scope must be non-empty")
		}
		if strings.ContainsAny(scope, "()\r\n") {
			return errors.New("scope must be non-empty and may not contain nested parentheses")
		}
	} else if strings.Contains(prefix, ")") {
		return errors.New("scope must be non-empty and may not contain nested parentheses")
	}
	if typ == "" {
		return errors.New("commit type must not be empty")
	}
	allowed := false
	for _, configured := range types {
		if strings.EqualFold(typ, configured) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("commit type %q is not allowed", typ)
	}
	return nil
}

func isMergeSubject(header string) bool {
	return strings.HasPrefix(header, "Merge ") || header == "Merge"
}

func isFixupSubject(header string) bool {
	return strings.HasPrefix(header, "fixup! ") || strings.HasPrefix(header, "squash! ")
}

func isRevertSubject(header string) bool {
	return strings.HasPrefix(header, "Revert ") || header == "Revert"
}
