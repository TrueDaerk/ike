; CSS highlights, adapted from tree-sitter/tree-sitter-css v0.25.0 (MIT).
; Reordered for ike's first-span-wins capture index: pseudo-selector
; patterns precede the generic class/property captures.

(comment) @comment

(tag_name) @tag
(nesting_selector) @tag
(universal_selector) @tag

"~" @operator
">" @operator
"+" @operator
"-" @operator
"*" @operator
"/" @operator
"=" @operator
"^=" @operator
"|=" @operator
"~=" @operator
"$=" @operator
"*=" @operator

"and" @operator
"or" @operator
"not" @operator
"only" @operator

(attribute_selector (plain_value) @string)

((property_name) @variable
 (#match? @variable "^--"))
((plain_value) @variable
 (#match? @variable "^--"))

(pseudo_element_selector (tag_name) @attribute)
(pseudo_class_selector (class_name) @attribute)

(class_name) @property
(id_name) @property
(namespace_name) @property
(property_name) @property
(feature_name) @property

(attribute_name) @attribute

(function_name) @function

"@media" @keyword
"@import" @keyword
"@charset" @keyword
"@namespace" @keyword
"@supports" @keyword
"@keyframes" @keyword
(at_keyword) @keyword
(to) @keyword
(from) @keyword
(important) @keyword

(string_value) @string
(color_value) @string.special

; The number span (integer_value/float_value) encloses its (unit) child and
; wins on position order, so the unit is deliberately not captured separately:
; "4px" colors uniformly as a number.
(integer_value) @number
(float_value) @number

[
  "#"
  ","
  "."
  ":"
  "::"
  ";"
] @punctuation.delimiter

[
  "{"
  ")"
  "("
  "}"
] @punctuation.bracket
