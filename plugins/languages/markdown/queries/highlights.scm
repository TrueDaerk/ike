; Markdown block highlights, adapted from tree-sitter-grammars/tree-sitter-markdown
; v0.5.3 (MIT, query originally from nvim-treesitter). Capture names remapped to
; ike's theme captures (see internal/theme/builtins.go): text.title -> function,
; punctuation.special/delimiter -> punctuation, text.uri -> attribute,
; text.reference -> label, literal blocks -> embedded. Ordered for ike's
; first-span-wins capture index: small specific nodes before the whole-block
; captures.

(atx_heading
  (inline) @function)

(setext_heading
  (paragraph) @function)

[
  (atx_h1_marker)
  (atx_h2_marker)
  (atx_h3_marker)
  (atx_h4_marker)
  (atx_h5_marker)
  (atx_h6_marker)
  (setext_h1_underline)
  (setext_h2_underline)
] @punctuation

(fenced_code_block_delimiter) @punctuation

(info_string
  (language) @attribute)

(link_title) @string

(link_destination) @attribute

(link_label) @label

[
  (list_marker_plus)
  (list_marker_minus)
  (list_marker_star)
  (list_marker_dot)
  (list_marker_parenthesis)
  (thematic_break)
] @punctuation

[
  (block_continuation)
  (block_quote_marker)
] @punctuation

(backslash_escape) @escape

(indented_code_block) @embedded

; The content node, not the whole fenced_code_block: the block starts at the
; same byte as its opening delimiter, and ike's capture index is first-wins —
; capturing the block would shadow the delimiter's punctuation.
(code_fence_content) @embedded
