; YAML highlights, adapted from tree-sitter-grammars/tree-sitter-yaml v0.7.2 (MIT).
; Reordered for ike's first-span-wins capture index: mapping-key patterns come
; before the generic scalar captures so keys stay @property. punctuation.* is
; remapped to ike's single @punctuation capture (see internal/theme/builtins.go).

(block_mapping_pair
  key: (flow_node
    [
      (double_quote_scalar)
      (single_quote_scalar)
    ] @property))

(block_mapping_pair
  key: (flow_node
    (plain_scalar
      (string_scalar) @property)))

(flow_mapping
  (_
    key: (flow_node
      [
        (double_quote_scalar)
        (single_quote_scalar)
      ] @property)))

(flow_mapping
  (_
    key: (flow_node
      (plain_scalar
        (string_scalar) @property))))

(boolean_scalar) @boolean

(null_scalar) @constant.builtin

[
  (double_quote_scalar)
  (single_quote_scalar)
  (block_scalar)
  (string_scalar)
] @string

[
  (integer_scalar)
  (float_scalar)
] @number

(comment) @comment

[
  (anchor_name)
  (alias_name)
] @label

(tag) @type

[
  (yaml_directive)
  (tag_directive)
  (reserved_directive)
] @attribute

(escape_sequence) @escape

[
  ","
  "-"
  ":"
  ">"
  "?"
  "|"
] @punctuation

[
  "["
  "]"
  "{"
  "}"
] @punctuation

[
  "*"
  "&"
  "---"
  "..."
] @punctuation
