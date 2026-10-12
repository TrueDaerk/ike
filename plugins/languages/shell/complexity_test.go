//go:build cgo

package langshell

import (
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/highlight/treetest"
)

// shellComplexityProbe exercises every kind and operator the spec names: a
// function with if / elif / else, for, the C-style for, while, until, case,
// an && list and a nested function, plus the two `function` spellings.
var shellComplexityProbe = []string{
	"#!/bin/bash",
	"f() {", // 1
	"  if [ \"$1\" ] && [ \"$2\" ] || true; then",
	"    echo a",
	"  elif [ \"$2\" ]; then",
	"    echo b",
	"  else",
	"    echo c",
	"  fi",
	"  for x in a b; do echo $x; done",
	"  for ((i=0;i<3;i++)); do :; done",
	"  while true; do break; done",
	"  until false; do break; done",
	"  case $1 in",
	"    a) echo a ;;",
	"    b|c) echo b ;;",
	"    *) echo d ;;",
	"  esac",
	"  [ -f x ] && echo y",
	"  g() { :; }", // 19
	"}",
	"function h { :; }",   // 21
	"function k() { :; }", // 22
}

func TestShellComplexitySpecKindsExist(t *testing.T) {
	root := highlight.SyntaxTree("shell", shellComplexityProbe)
	if root == nil {
		t.Fatal("no shell grammar")
	}
	kinds := treetest.Kinds(root)
	for _, k := range treetest.SpecKinds(shellComplexity) {
		if !kinds[k] {
			t.Errorf("spec names kind %q, which the probe never parses to", k)
		}
	}
	ops := treetest.Operators(root)
	for _, op := range shellComplexity.BoolOps {
		if !ops[op] {
			t.Errorf("spec names operator %q, which no probe node carries", op)
		}
	}
}

func TestShellComplexityProbeFunctions(t *testing.T) {
	root := highlight.SyntaxTree("shell", shellComplexityProbe)
	if root == nil {
		t.Fatal("no shell grammar")
	}
	got := treetest.Functions(root, strings.Join(shellComplexityProbe, "\n"), shellComplexity)
	want := []treetest.Function{
		// 1 + if, elif, for, c-style for, while, until, 3 case arms + the
		// condition's && and ||, the list's &&.
		{Name: "f", Kind: "function_definition", Line: 1, Params: 0, CCN: 13},
		{Name: "g", Kind: "function_definition", Line: 19, Params: 0, CCN: 1},
		{Name: "h", Kind: "function_definition", Line: 21, Params: 0, CCN: 1},
		{Name: "k", Kind: "function_definition", Line: 22, Params: 0, CCN: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("Functions = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("function %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
