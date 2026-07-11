package lang

import (
	"testing"
	"time"

	"ike/internal/config"
)

func TestExpandTemplateVariables(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	got := expandTemplate(
		"f=${FILENAME} n=${NAME} d=${DIR} p=${PACKAGE} date=${DATE} y=${YEAR}",
		"/proj/my-pkg/main.go", now)
	want := "f=main.go n=main d=my-pkg p=mypkg date=2026-07-11 y=2026"
	if got != want {
		t.Fatalf("expandTemplate = %q, want %q", got, want)
	}
}

func TestExpandTemplateEmpty(t *testing.T) {
	if got := expandTemplate("", "/x/y.go", time.Time{}); got != "" {
		t.Fatalf("empty template expanded to %q", got)
	}
}

func TestIdentifier(t *testing.T) {
	for in, want := range map[string]string{
		"my-pkg":  "mypkg",
		"My_Pkg":  "my_pkg",
		"3d":      "d",
		"--- 42":  "main", // digits cannot lead, nothing else survives
		"überapp": "überapp",
	} {
		if got := identifier(in); got != want {
			t.Errorf("identifier(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTemplateFor(t *testing.T) {
	Register(Language{
		ID:         "tmpllang",
		Extensions: []string{"tmpl"},
		Template:   "hello ${NAME}\n",
	})

	if got := TemplateFor("/proj/thing.tmpl"); got != "hello thing\n" {
		t.Fatalf("TemplateFor = %q", got)
	}
	// No language match: no template.
	if got := TemplateFor("/proj/thing.unknown-ext"); got != "" {
		t.Fatalf("TemplateFor(unknown) = %q", got)
	}
}

func TestTemplateForConfigOverride(t *testing.T) {
	Register(Language{
		ID:         "tmpllang2",
		Extensions: []string{"tp2"},
		Template:   "builtin\n",
	})
	pristine := config.Get() // fresh defaults before any Set in this package
	defer config.Set(pristine)

	c := config.Get()
	c.Lang["tmpllang2"] = map[string]string{"template": "custom ${FILENAME}"}
	config.Set(c)
	if got := TemplateFor("/p/a.tp2"); got != "custom a.tp2" {
		t.Fatalf("override: TemplateFor = %q", got)
	}

	// An explicitly empty override disables the built-in template.
	c.Lang["tmpllang2"]["template"] = ""
	config.Set(c)
	if got := TemplateFor("/p/a.tp2"); got != "" {
		t.Fatalf("disabled: TemplateFor = %q", got)
	}
}
