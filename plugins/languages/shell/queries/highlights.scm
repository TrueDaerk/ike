; Shell highlights, adapted from tree-sitter/tree-sitter-bash v0.25.1 (MIT).
; Reordered for ike's first-span-wins capture index: the function-definition
; name pattern precedes the generic command capture.

(function_definition name: (word) @function)

(command_name) @function

(variable_name) @property

[
  (string)
  (raw_string)
  (heredoc_body)
  (heredoc_start)
] @string

[
  "case"
  "do"
  "done"
  "elif"
  "else"
  "esac"
  "export"
  "fi"
  "for"
  "function"
  "if"
  "in"
  "select"
  "then"
  "unset"
  "until"
  "while"
] @keyword

(comment) @comment

(file_descriptor) @number

[
  (command_substitution)
  (process_substitution)
  (expansion)
] @embedded

[
  "$"
  "&&"
  ">"
  ">>"
  "<"
  "|"
] @operator

(
  (command (_) @constant)
  (#match? @constant "^-")
)
