// Package langpython registers Python: Tree-sitter highlighting, the pyright
// language server, and a toolchain detector that resolves the project interpreter
// (active venv / .venv / .python-version / pyproject) and hands its path to
// pyright so version-aware diagnostics check against the project's actual
// interpreter. Self-registers via init(); blank-imported in cmd/ike/main.go.
package langpython

import (
	_ "embed"

	"ike/internal/consthint"
	"ike/internal/escapes"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/permhint"
	"ike/plugins/languages/register"
)

//go:embed queries/python.scm
var query string

//go:embed queries/injections.scm
var injections string

func init() {
	register.Language(lang.Language{
		ID:         "python",
		Extensions: []string{"py", "pyi"},
		// Shebang fallback (#893): extensionless scripts; python3.12-style
		// version suffixes are stripped by the lookup.
		Interpreters: []string{"python", "python3"},
		Grammar:      grammar(),
		Server: &lang.ServerSpec{
			Language:    "python",
			Command:     "pyright-langserver",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"pyproject.toml", "setup.py", "setup.cfg", ".git"},
			Install:     []string{"npm", "install", "-g", "pyright"},
			// Pyright applies per-rule severity overrides natively (#1503);
			// exact-code lsp.diagnostics_severity rules land here.
			SeverityOverridesPath: []string{"python", "analysis", "diagnosticSeverityOverrides"},
		},
		Toolchain: toolchain{},
		// Network literals (#1653) inside string literals: a CIDR prefix
		// draws its range, a punycode host its decoded name. Permission
		// literals (#1656): the octal mode argument of os.chmod and friends
		// draws its symbolic form.
		Spans:       pythonSpans,
		LineComment: "#",
		IndentAfter: []string{":", "(", "[", "{"},
		// Postfix completion (#1913): `xs.for`, `flag.not`, `items.len`.
		// See postfix.go.
		Postfix:          postfixTemplates,
		PostfixExprNodes: postfixExprNodes,
		// Sticky-scroll scopes (#168).
		ScopeNodes: []string{"function_definition", "class_definition"},
		// Foldable regions (#144): definitions, compound statements,
		// multi-line collections and multi-line strings (docstrings).
		FoldNodes: []string{
			"function_definition", "class_definition", "if_statement",
			"for_statement", "while_statement", "with_statement",
			"try_statement", "match_statement", "dictionary", "list",
			"set", "tuple", "string",
		},
		// Test runner (#1911): pytest, following the Go seam (#1150). The
		// interpreter is the resolved project python, so `-m pytest` uses the
		// venv's pytest. A single test runs by -k name match (reaches class
		// methods, which a file::name node id would miss); the file scope
		// targets the file itself. -v makes the output one PASSED/FAILED line
		// per test for ParseOutput, --tb=short keeps failure locations one
		// `file.py:line:` line per frame.
		Test: &lang.TestSpec{
			FilePattern: `^(test_.*|.*_test)\.py$`,
			Pattern:     `^\s*(?:async\s+)?def\s+(?P<name>test[A-Za-z0-9_]*)\s*\(`,
			Kinds: map[string][]string{
				"": {"{interpreter}", "-m", "pytest", "{file}", "-k", "{name}"},
			},
			FileArgv:       []string{"{interpreter}", "-m", "pytest", "{file}"},
			Tool:           "python3",
			StructuredArgs: []string{"-v", "--tb=short"},
			ParseOutput:    parsePytest,
			FailedArgv:     []string{"{interpreter}", "-m", "pytest", "{file}", "-k", "{names}"},
			NamesJoin:      " or ",
		},
	})
}

// pythonSpans is the lang.Language.Spans hook: the secret masks (#1811) on
// assignments whose target names a credential, the unicode-escape stand-ins
// (#1620, #2334), the network-literal hints (#1653) inside string literals,
// the permission hints (#1656) inside the argument lists of the mode APIs,
// and the constant conceals (#1701) on CONST_CASE assignments.
//
// The masks come first: overlapping spans resolve first-covering-wins, so a
// masked value must outrank the hint another family would draw over the same
// columns (`self.timeout = 500` masks rather than reading as a duration).
func pythonSpans(lines []string) []lang.Span {
	out := append(maskSpans(lines), escapes.UnicodeSpansIn(lines, escapes.UnicodePython)...)
	out = append(out, nethint.QuotedSpans(lines)...)
	out = append(out, permhint.PythonSpans(lines)...)
	return append(out, consthint.PythonSpans(lines)...)
}
