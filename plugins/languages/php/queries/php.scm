[
  (php_tag)
  (php_end_tag)
] @tag

; Keywords

[
  "and"
  "as"
  "break"
  "case"
  "catch"
  "class"
  "clone"
  "const"
  "continue"
  "declare"
  "default"
  "do"
  "echo"
  "else"
  "elseif"
  "enddeclare"
  "endfor"
  "endforeach"
  "endif"
  "endswitch"
  "endwhile"
  "enum"
  "exit"
  "extends"
  "finally"
  "fn"
  "for"
  "foreach"
  "function"
  "global"
  "goto"
  "if"
  "implements"
  "include"
  "include_once"
  "instanceof"
  "insteadof"
  "interface"
  "match"
  "namespace"
  "new"
  "or"
  "print"
  "require"
  "require_once"
  "return"
  "switch"
  "throw"
  "trait"
  "try"
  "use"
  "while"
  "xor"
  "yield"
  "yield from"
  (abstract_modifier)
  (final_modifier)
  (readonly_modifier)
  (static_modifier)
  (visibility_modifier)
] @keyword

(function_static_declaration "static" @keyword)

; Namespace

(namespace_definition
  name: (namespace_name
    (name) @module))

(namespace_name
  (name) @module)

(namespace_use_clause
  [
    (name) @type
    (qualified_name
      (name) @type)
    alias: (name) @type
  ])

(namespace_use_clause
  type: "function"
  [
    (name) @function
    (qualified_name
      (name) @function)
    alias: (name) @function
  ])

(namespace_use_clause
  type: "const"
  [
    (name) @constant
    (qualified_name
      (name) @constant)
    alias: (name) @constant
  ])

(relative_name "namespace" @module.builtin)

; Variables

(relative_scope) @variable.builtin

(variable_name) @variable

(method_declaration name: (name) @constructor
  (#eq? @constructor "__construct"))

(object_creation_expression [
  (name) @constructor
  (qualified_name (name) @constructor)
  (relative_name (name) @constructor)
])

((name) @constant
 (#match? @constant "^_?[A-Z][A-Z\\d_]+$"))
((name) @constant.builtin
 (#match? @constant.builtin "^__[A-Z][A-Z\d_]+__$"))
(const_declaration (const_element (name) @constant))

; Types

(primitive_type) @type.builtin
(cast_type) @type.builtin
(named_type [
  (name) @type
  (qualified_name (name) @type)
  (relative_name (name) @type)
]) @type
(named_type (name) @type.builtin
  (#any-of? @type.builtin "static" "self"))

(scoped_call_expression
  scope: [
    (name) @type
    (qualified_name (name) @type)
    (relative_name (name) @type)
  ])

; Functions

(array_creation_expression "array" @function.builtin)
(list_literal "list" @function.builtin)
(exit_statement "exit" @function.builtin "(")

(method_declaration
  name: (name) @function.method)

(function_call_expression
  function: [
    (qualified_name (name))
    (relative_name (name))
    (name)
  ] @function)

(scoped_call_expression
  name: (name) @function)

(member_call_expression
  name: (name) @function.method)

(function_definition
  name: (name) @function)

; Member

(property_element
  (variable_name) @property)

(member_access_expression
  name: (variable_name (name)) @property)
(member_access_expression
  name: (name) @property)

; Basic tokens

; Strings decompose into their parts (#1466): whole-node (encapsed_string) /
; (heredoc_body) spans would start before any interpolation inside them and
; win the first-covering lookup, painting `{$x}` and `$var` flat. With only
; the delimiters and literal content captured, interpolated expressions
; highlight through the normal captures above. Nowdocs never interpolate, so
; the whole node stays one capture.
(string) @string
(string_content) @string
(encapsed_string "\"" @string)
(heredoc "<<<" @string)
(heredoc "\"" @string)
(heredoc_start) @string
(heredoc_end) @string
(nowdoc) @string
(escape_sequence) @string.escape

; Complex interpolation braces: "abc{$x}def" and heredoc bodies.
(encapsed_string "{" @punctuation.special)
(encapsed_string "}" @punctuation.special)
(heredoc_body "{" @punctuation.special)
(heredoc_body "}" @punctuation.special)

; ${x} form (deprecated in 8.2, still parsed).
(dynamic_variable_name "$" @punctuation.special)
(dynamic_variable_name "{" @punctuation.special)
(dynamic_variable_name "}" @punctuation.special)
(dynamic_variable_name (name) @variable)

; Unbraced array index in simple interpolation: "$arr[key]".
(encapsed_string (subscript_expression (name) @property))
(heredoc_body (subscript_expression (name) @property))
(boolean) @constant.builtin
(null) @constant.builtin
(integer) @number
(float) @number
(comment) @comment

((name) @variable.builtin
 (#eq? @variable.builtin "this"))

"$" @operator
