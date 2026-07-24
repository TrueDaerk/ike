; Makefile highlights, adapted from alemuller/tree-sitter-make
; (main @ a4b9187, MIT — see grammar/LICENSE) to ike's theme captures (see
; internal/theme/builtins.go): targets -> function, variables -> variable,
; automatic variables ($@, $<, …) -> variable.builtin, implicit-rule /
; special variables and special targets (.PHONY, …) -> constant.builtin,
; directives and conditionals -> keyword, make functions -> function.
; Recipe bodies are deliberately not captured here: (shell_text) fragments
; highlight as shell via queries/injections.scm (#1136). Specialised
; #match? patterns come before their general fallback — CaptureAt is
; first-covering-wins.

;; Special variables of implicit rules and special targets, before the
;; generic variable/target captures so they win.
[
 "VPATH"
 ".RECIPEPREFIX"
] @constant.builtin

(variable_assignment
  name: (word) @constant.builtin
        (#match? @constant.builtin "^(AR|AS|CC|CXX|CPP|FC|M2C|PC|CO|GET|LEX|YACC|LINT|MAKEINFO|TEX|TEXI2DVI|WEAVE|CWEAVE|TANGLE|CTANGLE|RM|ARFLAGS|ASFLAGS|CFLAGS|CXXFLAGS|COFLAGS|CPPFLAGS|FFLAGS|GFLAGS|LDFLAGS|LDLIBS|LFLAGS|YFLAGS|PFLAGS|RFLAGS|LINTFLAGS|PRE_INSTALL|POST_INSTALL|NORMAL_INSTALL|PRE_UNINSTALL|POST_UNINSTALL|NORMAL_UNINSTALL|MAKEFILE_LIST|MAKE_RESTARTS|MAKE_TERMOUT|MAKE_TERMERR|\.DEFAULT_GOAL|\.RECIPEPREFIX|\.EXTRA_PREREQS)$"))

(variable_reference
  (word) @constant.builtin
  (#match? @constant.builtin "^(AR|AS|CC|CXX|CPP|FC|M2C|PC|CO|GET|LEX|YACC|LINT|MAKEINFO|TEX|TEXI2DVI|WEAVE|CWEAVE|TANGLE|CTANGLE|RM|ARFLAGS|ASFLAGS|CFLAGS|CXXFLAGS|COFLAGS|CPPFLAGS|FFLAGS|GFLAGS|LDFLAGS|LDLIBS|LFLAGS|YFLAGS|PFLAGS|RFLAGS|LINTFLAGS|PRE_INSTALL|POST_INSTALL|NORMAL_INSTALL|PRE_UNINSTALL|POST_UNINSTALL|NORMAL_UNINSTALL|MAKEFILE_LIST|MAKE_RESTARTS|MAKE_TERMOUT|MAKE_TERMERR|\.DEFAULT_GOAL|\.RECIPEPREFIX|\.EXTRA_PREREQS|\.VARIABLES|\.FEATURES|\.INCLUDE_DIRS|\.LOADED)$"))

;; Special targets (.PHONY, .SUFFIXES, …), before the generic target capture.
(targets
  (word) @constant.builtin
  (#match? @constant.builtin "^\.(PHONY|SUFFIXES|DEFAULT|PRECIOUS|INTERMEDIATE|SECONDARY|SECONDEXPANSION|DELETE_ON_ERROR|IGNORE|LOW_RESOLUTION_TIME|SILENT|EXPORT_ALL_VARIABLES|NOTPARALLEL|ONESHELL|POSIX)$"))

;; Rule targets.
(targets
  (word) @function)

;; Variables.
(variable_assignment
  name: (word) @variable)

(variable_reference
  (word) @variable)

(automatic_variable
 [ "@" "%" "<" "?" "^" "+" "/" "*" "D" "F"] @variable.builtin)

;; Directives and conditionals.
[
 "ifeq"
 "ifneq"
 "ifdef"
 "ifndef"
 "else"
 "endif"
 "define"
 "endef"
 "vpath"
 "undefine"
 "export"
 "unexport"
 "override"
 "private"
 "include"
 "sinclude"
 "-include"
] @keyword

;; Make functions ($(patsubst …), $(shell …), $(if …), …).
[
 "if"
 "or"
 "and"
 "foreach"
 "subst"
 "patsubst"
 "strip"
 "findstring"
 "filter"
 "filter-out"
 "sort"
 "word"
 "words"
 "wordlist"
 "firstword"
 "lastword"
 "dir"
 "notdir"
 "suffix"
 "basename"
 "addsuffix"
 "addprefix"
 "join"
 "wildcard"
 "realpath"
 "abspath"
 "call"
 "eval"
 "file"
 "value"
 "shell"
 "error"
 "warning"
 "info"
] @function

;; Operators and punctuation.
[
 "="
 ":="
 "::="
 "?="
 "+="
 "!="
 "@"
 "-"
 "+"
] @operator

[
 "("
 ")"
 "{"
 "}"
] @punctuation

[
 ":"
 "&:"
 "::"
 "|"
 ";"
 "\""
 "'"
 ","
] @punctuation

[
 "$"
 "$$"
] @punctuation

(string) @string
(raw_text) @string

(comment) @comment
