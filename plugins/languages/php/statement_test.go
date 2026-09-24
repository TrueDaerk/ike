package langphp

import (
	"reflect"
	"testing"

	"ike/internal/lang"
)

func TestCompleteStatement(t *testing.T) {
	block := func(head, closer string) lang.StatementCompletion {
		return lang.StatementCompletion{Head: head, Body: true, Tail: []string{closer}}
	}
	simple := func(head string) lang.StatementCompletion { return lang.StatementCompletion{Head: head} }
	for _, tc := range []struct {
		line string
		want lang.StatementCompletion
		ok   bool
	}{
		{"function query()", block("function query() {", "}"), true},
		{"function query(", block("function query() {", "}"), true},
		{"    public static function query(array $a", block("    public static function query(array $a) {", "}"), true},
		{"abstract class Foo extends Bar", block("abstract class Foo extends Bar {", "}"), true},
		{"final readonly class Point", block("final readonly class Point {", "}"), true},
		{"interface Shape", block("interface Shape {", "}"), true},
		{"trait Loggable", block("trait Loggable {", "}"), true},
		{"enum Suit: string", block("enum Suit: string {", "}"), true},
		{"if ($x", block("if ($x) {", "}"), true},
		{"if($x)", block("if($x) {", "}"), true},
		{"} elseif ($y)", block("} elseif ($y) {", "}"), true},
		{"} else if ($y)", block("} else if ($y) {", "}"), true},
		{"} else", block("} else {", "}"), true},
		{"for ($i = 0; $i < 3; $i++)", block("for ($i = 0; $i < 3; $i++) {", "}"), true},
		{"foreach ($xs as $x)", block("foreach ($xs as $x) {", "}"), true},
		{"while (true)", block("while (true) {", "}"), true},
		{"switch ($x)", block("switch ($x) {", "}"), true},
		{"try", block("try {", "}"), true},
		{"} catch (Throwable $e)", block("} catch (Throwable $e) {", "}"), true},
		{"} finally", block("} finally {", "}"), true},
		// Already complete headers only open the body.
		{"function query() {", block("function query() {", "}"), true},
		{"if ($x) {  ", block("if ($x) {", "}"), true},
		// Expression-shaped blocks close with "};".
		{"$x = match($y)", block("$x = match($y) {", "};"), true},
		{"$f = function ($a) use ($b)", block("$f = function ($a) use ($b) {", "};"), true},
		{"return match (true)", block("return match (true) {", "};"), true},
		{"$f = function ($a) {", block("$f = function ($a) {", "};"), true},
		// A closure passed to a call keeps the call open; its closing line
		// closes both.
		{"array_map(function ($x)", block("array_map(function ($x) {", "});"), true},
		{"$y = array_map(function ($x)", block("$y = array_map(function ($x) {", "});"), true},
		// Simple statements get their semicolon.
		{"$x = foo()", simple("$x = foo();"), true},
		{"$x = foo(", simple("$x = foo();"), true},
		{"    return $x", simple("    return $x;"), true},
		{"use App\\Models\\User", simple("use App\\Models\\User;"), true},
		{"$x = foo();", simple("$x = foo();"), true},
		{"case 1:", simple("case 1:"), true},
		{"}", simple("}"), true},
		{"$f = fn($x) => $x + 1", simple("$f = fn($x) => $x + 1;"), true},
		// Left alone: tags, comments, blank lines.
		{"<?php", lang.StatementCompletion{}, false},
		{"?>", lang.StatementCompletion{}, false},
		{"// $x = 1", lang.StatementCompletion{}, false},
		{" * @param int $x", lang.StatementCompletion{}, false},
		{"", lang.StatementCompletion{}, false},
	} {
		got, ok := toolchain{}.CompleteStatement(tc.line)
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %+v, %v; want %+v, %v", tc.line, got, ok, tc.want, tc.ok)
		}
	}
}

func TestStatementCompleterRegistered(t *testing.T) {
	if !lang.SupportsStatementCompletion("php") {
		t.Fatal("php does not support statement completion")
	}
}
