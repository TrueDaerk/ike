; HTTP request file highlights, adapted from rest-nvim/tree-sitter-http (MIT).
; Capture names remapped to ike's theme captures (see internal/theme/builtins.go):
; function.method -> function, string.special.url/path -> string, @spell dropped,
; punctuation.bracket/delimiter -> punctuation. ike addition: header values -> property.

; Methods
(method) @function

; Headers
(header
  name: (_) @constant)
(header
  value: (_) @property)

; Variables
(variable_declaration
  name: (identifier) @variable)
(variable
  name: (_) @variable)

; Operators
(comment
  "=" @operator)
(variable_declaration
  "=" @operator)

; Keywords
(comment
  "@" @keyword
  name: (_) @keyword)

; Literals
(request
  url: (_) @string)

(http_version) @constant

; Response
(status_code) @number
(status_text) @string

; Punctuation
[
  "{{"
  "}}"
] @punctuation

(header
  ":" @punctuation)

; External body reference
(external_body
  path: (_) @string)

; Comments (### request separators included)
[
  (comment)
  (request_separator)
] @comment
