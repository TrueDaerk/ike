; Markdown inline highlights, adapted from tree-sitter-grammars/tree-sitter-markdown
; v0.5.3 (MIT, query originally from nvim-treesitter). Capture names remapped to
; ike's theme captures: text.literal -> string, text.uri -> attribute,
; text.reference -> label, punctuation.delimiter -> punctuation; emphasis gets
; color proxies until #881 ships real bold/italic rendering (strong -> keyword,
; emphasis -> attribute). Ordered for ike's first-span-wins capture index:
; delimiters and specific spans before the enclosing emphasis captures.

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

(strong_emphasis) @keyword

(emphasis) @attribute
