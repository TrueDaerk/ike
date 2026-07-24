; go.work highlights, adapted from omertuc/tree-sitter-go-work
; (main @ 949a8a4, MIT — see grammar_gowork/LICENSE). Capture names mapped to
; ike's theme captures (see internal/theme/builtins.go) the same way
; gomod.scm is: directory/module paths -> type, versions and quoted strings
; -> string, block parens -> punctuation.

[
  "go"
  "use"
  "replace"
] @keyword

"=>" @operator

(comment) @comment

(module_path) @type

(file_path) @type

[
  (version)
  (go_version)
  (interpreted_string_literal)
  (raw_string_literal)
] @string

(escape_sequence) @escape

[
  "("
  ")"
] @punctuation
