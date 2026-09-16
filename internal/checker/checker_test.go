package checker

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseConfigStrictAndDefaults(t *testing.T) {
	cfg, err := ParseConfig([]byte(`{"types":["FEAT","fix"],"maxHeaderLength":12,"ignoreMergeCommits":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Types, []string{"FEAT", "fix"}) || cfg.MaxHeaderLength != 12 || cfg.IgnoreMergeCommits || !cfg.IgnoreFixupCommits {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	for _, input := range []string{`{"unknown":true}`, `{"types":[]}`, `{"types":null}`, `{"maxHeaderLength":-1}`, `{"types":["feat","FEAT"]}`, `{"types":["feat"]} trailing`, `null`} {
		if _, err := ParseConfig([]byte(input)); err == nil {
			t.Errorf("ParseConfig(%q) unexpectedly succeeded", input)
		}
	}
	if _, err := ParseConfig([]byte("\n")); err == nil {
		t.Fatal("empty config unexpectedly accepted")
	}
}

func TestValidateGrammarAndIgnoredSubjects(t *testing.T) {
	cfg := DefaultConfig()
	valid := []string{
		"feat: add it",
		"FEAT(core team)!: no casing rule",
		"fix(core): subject may end.",
		"feat: unicode café",
		"feat: body\r\nmore",
	}
	for _, msg := range valid {
		if errs := Validate(msg, cfg); len(errs) != 0 {
			t.Errorf("Validate(%q) = %v", msg, errs)
		}
	}
	for _, msg := range []string{"Merge branch 'x'", `Revert "feat: old"`, "fixup! feat: old", "squash! fix: old"} {
		if errs := Validate(msg, cfg); len(errs) != 0 {
			t.Errorf("ignored Validate(%q) = %v", msg, errs)
		}
	}
	if errs := Validate("Merge branch 'x'", Config{Types: []string{"feat"}}); len(errs) == 0 {
		t.Error("merge should be checked when ignoreMergeCommits is false")
	}
	if errs := Validate("fixup! feat: old", Config{Types: []string{"feat"}, IgnoreFixupCommits: false}); len(errs) == 0 {
		t.Error("fixup should be checked when ignoreFixupCommits is false")
	}
	if errs := Validate("feat(): empty scope", cfg); len(errs) == 0 {
		t.Error("empty scope unexpectedly accepted")
	}
	if errs := Validate("feat(foo(bar)): nested", cfg); len(errs) == 0 {
		t.Error("nested scope unexpectedly accepted")
	}
	if errs := Validate("feat(   ): blank scope", cfg); len(errs) == 0 {
		t.Error("whitespace-only scope unexpectedly accepted")
	}
	if errs := Validate("feat:   ", cfg); len(errs) == 0 {
		t.Error("whitespace-only subject unexpectedly accepted")
	}
	if errs := Validate("feat: "+strings.Repeat("é", 3), Config{Types: []string{"feat"}, MaxHeaderLength: 10}); len(errs) != 0 {
		t.Errorf("rune length unexpectedly rejected: %v", errs)
	}
}

func TestCheckAlwaysReturnsErrorsArray(t *testing.T) {
	got := Check([]Commit{{SHA: "a", Message: "feat: okay"}}, DefaultConfig())
	if len(got) != 1 || got[0].Errors == nil || len(got[0].Errors) != 0 {
		t.Fatalf("unexpected result: %#v", got)
	}
}
