package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	commitguard "github.com/codywilliamson/commit-guard"
	"github.com/codywilliamson/commit-guard/internal/checker"
	"github.com/codywilliamson/commit-guard/internal/history"
	"github.com/codywilliamson/commit-guard/internal/setup"
)

func main() {
	args := os.Args[1:]
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if name == "commit-msg" || name == "pre-push" {
		args = append([]string{"hook", name}, args...)
	}
	os.Exit(run(args, os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(stdout, "commit-guard "+commitguard.Version()+"\n\nCommands: install, sync, status, uninstall, check, hook, version\ncheck: --file PATH | --batch (JSON messages on stdin), --config PATH, --json\ninstall: --ref VERSION_OR_SHA --mode commits|title --ci-only --no-updates\nLocal hooks never download updates. Run sync explicitly after accepting an update PR.")
		return 0
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintln(stdout, commitguard.Version())
		return 0
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "commit-guard: "+err.Error()); return 2 }
	switch args[0] {
	case "check":
		fs := flag.NewFlagSet("check", flag.ContinueOnError)
		fs.SetOutput(stderr)
		file := fs.String("file", "", "message file")
		batch := fs.Bool("batch", false, "JSON batch on stdin")
		config := fs.String("config", "", "policy file")
		jsonOutput := fs.Bool("json", false, "JSON report")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if fs.NArg() != 0 {
			return fail(fmt.Errorf("unexpected check arguments"))
		}
		if *file != "" && *batch {
			return fail(fmt.Errorf("choose --file or --batch"))
		}
		if *config == "" {
			root, err := setup.Root()
			if err != nil {
				root = "."
			}
			*config = filepath.Join(root, ".commit-guard.json")
		}
		cfg, err := checker.LoadConfig(*config)
		if err != nil {
			return fail(err)
		}
		var messages []checker.Commit
		if *batch {
			decoder := json.NewDecoder(io.LimitReader(stdin, 64*1024*1024))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&messages); err != nil {
				return fail(fmt.Errorf("invalid message batch: %w", err))
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				return fail(fmt.Errorf("unexpected data after message batch"))
			}
		} else {
			var data []byte
			if *file != "" {
				data, err = os.ReadFile(*file)
			} else {
				data, err = io.ReadAll(io.LimitReader(stdin, 16*1024*1024))
			}
			if err != nil {
				return fail(err)
			}
			messages = []checker.Commit{{SHA: "message", Message: string(data)}}
		}
		return report(messages, cfg, *jsonOutput, stdout, stderr)
	case "hook":
		if len(args) < 2 {
			return fail(fmt.Errorf("hook requires commit-msg or pre-push"))
		}
		// Git invokes these hooks at the working-tree root.
		root, err := os.Getwd()
		if err != nil {
			return fail(err)
		}
		home := os.Getenv("COMMIT_GUARD_HOME")
		if home == "" {
			home, err = setup.Home(root)
			if err != nil {
				return fail(err)
			}
		}
		if err := setup.CheckVersionAt(root, home, commitguard.Version()); err != nil {
			return fail(err)
		}
		cfg, err := checker.LoadConfig(filepath.Join(root, ".commit-guard.json"))
		if err != nil {
			return fail(err)
		}
		var messages []checker.Commit
		switch args[1] {
		case "commit-msg":
			if len(args) != 3 {
				return fail(fmt.Errorf("commit-msg requires a message file"))
			}
			data, err := os.ReadFile(args[2])
			if err != nil {
				return fail(err)
			}
			messages = []checker.Commit{{SHA: "message", Message: string(data)}}
		case "pre-push":
			if len(args) < 3 {
				return fail(fmt.Errorf("pre-push requires the remote name"))
			}
			messages, err = history.Outgoing(root, args[2], stdin)
			if err != nil {
				return fail(err)
			}
		default:
			return fail(fmt.Errorf("unknown hook %q", args[1]))
		}
		return report(messages, cfg, false, stdout, stderr)
	case "install", "sync", "status", "uninstall":
		root, err := setup.Root()
		if err != nil {
			return fail(err)
		}
		switch args[0] {
		case "install":
			fs := flag.NewFlagSet("install", flag.ContinueOnError)
			fs.SetOutput(stderr)
			opts := setup.Options{}
			fs.StringVar(&opts.Ref, "ref", "", "release reference")
			fs.StringVar(&opts.Mode, "mode", "commits", "validation mode")
			fs.BoolVar(&opts.CIOnly, "ci-only", false, "do not install hooks")
			fs.BoolVar(&opts.NoUpdates, "no-updates", false, "do not configure Dependabot")
			if err := fs.Parse(args[1:]); err != nil {
				return 2
			}
			if fs.NArg() != 0 {
				return fail(fmt.Errorf("unexpected install arguments"))
			}
			err = setup.Install(root, commitguard.Version(), opts)
		case "sync":
			if len(args) != 1 {
				return fail(fmt.Errorf("sync takes no arguments; the repository workflow selects the version"))
			}
			err = setup.Sync(root, commitguard.Version(), true)
		case "status":
			err = setup.Status(root, commitguard.Version())
		case "uninstall":
			err = setup.Uninstall(root)
		}
		if err != nil {
			return fail(err)
		}
		return 0
	default:
		return fail(fmt.Errorf("unknown command %q; run commit-guard help", args[0]))
	}
}

func report(messages []checker.Commit, cfg checker.Config, jsonOutput bool, stdout, stderr io.Writer) int {
	results := checker.Check(messages, cfg)
	valid := true
	for _, result := range results {
		if len(result.Errors) == 0 {
			continue
		}
		valid = false
		if !jsonOutput {
			fmt.Fprintf(stderr, "commit-guard: %s: %s\n", result.SHA, strings.Join(result.Errors, "; "))
		}
	}
	if jsonOutput {
		json.NewEncoder(stdout).Encode(struct {
			Valid   bool             `json:"valid"`
			Results []checker.Result `json:"results"`
		}{valid, results})
	}
	if !valid {
		if !jsonOutput {
			fmt.Fprintln(stderr, "Expected: type(scope): description, for example feat(cli): add offline checks")
		}
		return 1
	}
	return 0
}
