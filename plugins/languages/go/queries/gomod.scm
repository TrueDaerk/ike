; go.mod / go.work highlights, adapted from camdencheek/tree-sitter-go-mod
; (main @ 2e88687, MIT — see grammar/LICENSE). Capture names mapped to ike's
; theme captures (see internal/theme/builtins.go): module paths -> type,
; versions and paths -> string, block parens -> punctuation.

[
  "module"
  "go"
  "require"
  "replace"
  "exclude"
  "retract"
  "toolchain"
  "tool"
  "ignore"
] @keyword

"=>" @operator

(comment) @comment

(module_path) @type

(file_path) @string

[
  (version)
  (go_version)
  (toolchain_name)
] @string

(escape_sequence) @escape

[
  "("
  ")"
] @punctuation
