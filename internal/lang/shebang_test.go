package lang

import "testing"

func TestForShebang(t *testing.T) {
	Register(Language{
		ID:           "shebanglang",
		Extensions:   []string{"sbl"},
		Interpreters: []string{"sbl", "sbl3"},
	})

	for _, tc := range []struct {
		line string
		want string // "" = no match
	}{
		{"#!/bin/sbl", "shebanglang"},
		{"#!/usr/local/bin/sbl3", "shebanglang"},
		{"#! /bin/sbl", "shebanglang"},                       // space after #!
		{"#!/usr/bin/env sbl", "shebanglang"},                // env form
		{"#!/usr/bin/env -S sbl run --quiet", "shebanglang"}, // env -S: first non-flag word
		{"#!/usr/bin/env -i FOO=bar sbl", "shebanglang"},     // flags + assignments skipped
		{"#!/bin/sbl3.12", "shebanglang"},                    // version suffix stripped
		{"#!/usr/bin/env sbl3.12.1", "shebanglang"},
		{"#!/bin/unknowninterp", ""},
		{"#!/usr/bin/env", ""}, // env with nothing after it
		{"plain first line", ""},
		{"", ""},
		{"#!", ""},
	} {
		l, ok := ForShebang(tc.line)
		if tc.want == "" {
			if ok {
				t.Errorf("ForShebang(%q) = %s, want no match", tc.line, l.ID)
			}
			continue
		}
		if !ok || l.ID != tc.want {
			t.Errorf("ForShebang(%q) = %v/%v, want %s", tc.line, l.ID, ok, tc.want)
		}
	}
}

// TestAssociatePath: a sniffed association wins over the static indexes and
// re-associating overwrites.
func TestAssociatePath(t *testing.T) {
	Register(Language{ID: "assoc-a", Extensions: []string{"assoca"}})
	Register(Language{ID: "assoc-b", Extensions: []string{"assocb"}})

	if _, ok := ByPath("/tmp/assoc-deploy"); ok {
		t.Fatal("extensionless path must not resolve before association")
	}
	AssociatePath("/tmp/assoc-deploy", "assoc-a")
	if l, ok := ByPath("/tmp/assoc-deploy"); !ok || l.ID != "assoc-a" {
		t.Errorf("after AssociatePath: %v/%v, want assoc-a", l.ID, ok)
	}
	AssociatePath("/tmp/assoc-deploy", "assoc-b")
	if l, _ := ByPath("/tmp/assoc-deploy"); l.ID != "assoc-b" {
		t.Errorf("re-association: %v, want assoc-b", l.ID)
	}
	// Other paths are untouched.
	if _, ok := ByPath("/tmp/assoc-other"); ok {
		t.Error("association must be per exact path")
	}
}
