; HTML highlights, adapted from tree-sitter/tree-sitter-html v0.23.2 (MIT).
; tag.error and punctuation.bracket resolve via the theme's dotted fallback.

(tag_name) @tag
(erroneous_end_tag_name) @tag.error
(doctype) @constant
(attribute_name) @attribute
(attribute_value) @string
(comment) @comment

[
  "<"
  ">"
  "</"
  "/>"
] @punctuation.bracket
