; JSON highlights, adapted from tree-sitter/tree-sitter-json v0.24.8 (MIT).
; Capture names remapped to ike's theme captures (see internal/theme/builtins.go):
;   string.special.key -> property.

(pair
  key: (_) @property)

(string) @string

(number) @number

[
  (null)
  (true)
  (false)
] @constant.builtin

(escape_sequence) @escape

(comment) @comment
