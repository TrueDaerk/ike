package langweb

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
		{"export default async function main(args: string[])", block("export default async function main(args: string[]) {", "}"), true},
		{"class Foo extends Bar", block("class Foo extends Bar {", "}"), true},
		{"export interface Shape", block("export interface Shape {", "}"), true},
		{"enum Color", block("enum Color {", "}"), true},
		{"  if (x", block("  if (x) {", "}"), true},
		{"} else if (y)", block("} else if (y) {", "}"), true},
		{"} else", block("} else {", "}"), true},
		{"for (const x of xs)", block("for (const x of xs) {", "}"), true},
		{"while (true)", block("while (true) {", "}"), true},
		{"switch (x)", block("switch (x) {", "}"), true},
		{"try", block("try {", "}"), true},
		{"} catch (e)", block("} catch (e) {", "}"), true},
		{"} finally", block("} finally {", "}"), true},
		{"  private render(): void", simple("  private render(): void;"), true},
		// Arrow functions about to open a block body.
		{"const f = (a, b) =>", block("const f = (a, b) => {", "};"), true},
		{"const f = async (a) =>", block("const f = async (a) => {", "};"), true},
		{"xs.forEach((x) =>", block("xs.forEach((x) => {", "});"), true},
		{"xs.forEach((x) => {", block("xs.forEach((x) => {", "});"), true},
		{"promise.then(function (r)", block("promise.then(function (r) {", "});"), true},
		{"return (x) =>", block("return (x) => {", "};"), true},
		// Expression-shaped blocks close with "};".
		{"const f = function (a)", block("const f = function (a) {", "};"), true},
		{"module.exports = class Foo", block("module.exports = class Foo {", "};"), true},
		// Already complete headers only open the body.
		{"function query() {", block("function query() {", "}"), true},
		{"const f = () => {", block("const f = () => {", "};"), true},
		{"const o = {", block("const o = {", "}"), true},
		// Simple statements get their semicolon.
		{"const x = foo()", simple("const x = foo();"), true},
		{"const x = foo(", simple("const x = foo();"), true},
		{"import { a } from './a'", simple("import { a } from './a';"), true},
		{"    return x", simple("    return x;"), true},
		{"const x = foo();", simple("const x = foo();"), true},
		{"default:", simple("default:"), true},
		{"}", simple("}"), true},
		// Left alone: comments and blank lines.
		{"// if (x)", lang.StatementCompletion{}, false},
		{"/* note */", lang.StatementCompletion{}, false},
		{"", lang.StatementCompletion{}, false},
	} {
		got, ok := tsToolchain{}.CompleteStatement(tc.line)
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %+v, %v; want %+v, %v", tc.line, got, ok, tc.want, tc.ok)
		}
	}
}

func TestStatementCompleterRegistered(t *testing.T) {
	if !lang.SupportsStatementCompletion("typescript") {
		t.Fatal("typescript does not support statement completion")
	}
}
