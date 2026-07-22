; Markdown inline highlights, adapted from tree-sitter-grammars/tree-sitter-markdown
; v0.5.3 (MIT, query originally from nvim-treesitter). Capture names remapped to
; ike's theme captures: text.literal -> string, text.uri -> attribute,
; text.reference -> label, punctuation.delimiter -> punctuation.
;
; Rich rendering (#881): emphasis nodes carry markup.* captures — the editor
; maps those to terminal text attributes (bold/italic/strikethrough), not
; colors. @conceal captures mark the marker chrome (delimiters, link
; brackets + destination) the editor hides on non-cursor lines; they are
; split out of the style index, so a node may carry both @punctuation (raw
; cursor-line styling) and @conceal.
;
; Ordered for ike's first-span-wins capture index: delimiters and specific
; spans before the enclosing emphasis captures.

[
  (emphasis_delimiter)
  (code_span_delimiter)
] @punctuation

[
  (code_span)
  (link_title)
] @string

[
  (link_destination)
  (uri_autolink)
] @attribute

[
  (link_label)
  (link_text)
  (image_description)
] @label

[
  (backslash_escape)
  (hard_line_break)
] @escape

(image
  [
    "!"
    "["
    "]"
    "("
    ")"
  ] @punctuation)

(inline_link
  [
    "["
    "]"
    "("
    ")"
  ] @punctuation)

(shortcut_link
  [
    "["
    "]"
  ] @punctuation)

(strong_emphasis) @markup.bold

(emphasis) @markup.italic

(strikethrough) @markup.strikethrough

; --- conceal chrome (#881) — kept last: the conceal channel is split out of
; the style index before first-span-wins applies, so order only matters among
; the style captures above.

[
  (emphasis_delimiter)
  (code_span_delimiter)
] @conceal

(inline_link
  [
    "["
    "]"
    "("
    ")"
  ] @conceal)

(inline_link
  (link_destination) @conceal)

(image
  [
    "!"
    "["
    "]"
    "("
    ")"
  ] @conceal)

(image
  (link_destination) @conceal)

(shortcut_link
  [
    "["
    "]"
  ] @conceal)
