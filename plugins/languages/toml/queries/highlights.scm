; TOML highlights, adapted from tree-sitter-grammars/tree-sitter-toml v0.7.0 (MIT).
; Capture names remapped to ike's theme captures (see internal/theme/builtins.go):
;   table headers -> type, keys -> property, date/time -> constant,
;   punctuation.delimiter/bracket -> punctuation.

(table (bare_key) @type)
(table (dotted_key (bare_key) @type))
(table (quoted_key) @type)
(table_array_element (bare_key) @type)
(table_array_element (dotted_key (bare_key) @type))
(table_array_element (quoted_key) @type)

(pair (bare_key) @property)
(pair (dotted_key (bare_key) @property))
(pair (quoted_key) @property)

(boolean) @boolean

(comment) @comment

(string) @string

(escape_sequence) @escape

[
  (integer)
  (float)
] @number

[
  (offset_date_time)
  (local_date_time)
  (local_date)
  (local_time)
] @constant

[
  "."
  ","
] @punctuation

"=" @operator

[
  "["
  "]"
  "[["
  "]]"
  "{"
  "}"
] @punctuation
