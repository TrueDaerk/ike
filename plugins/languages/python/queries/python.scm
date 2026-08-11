; Captures are first-span-wins (highlight.Index.CaptureAt), and the query
; cursor yields same-node captures in pattern order — so specific patterns
; must precede broader ones, and the identifier catch-all comes last (#724).

; Decorators (#928): only the @ sigil and the (dotted) name get the decorator
; color — never the whole (decorator) node, whose span would enclose the
; argument list and win on position order, painting strings/kwargs/parens in
; one monochrome block. Arguments highlight through the normal call rules.

(decorator "@" @function)

(decorator
  (identifier) @function)

(decorator
  (attribute) @function)

(decorator
  (call
    function: (identifier) @function))

(decorator
  (call
    function: (attribute) @function))

; Function calls

; Builtin functions (before the generic call pattern, which would shadow them)

((call
  function: (identifier) @function.builtin)
 (#match?
   @function.builtin
   "^(abs|all|any|ascii|bin|bool|breakpoint|bytearray|bytes|callable|chr|classmethod|compile|complex|delattr|dict|dir|divmod|enumerate|eval|exec|filter|float|format|frozenset|getattr|globals|hasattr|hash|help|hex|id|input|int|isinstance|issubclass|iter|len|list|locals|map|max|memoryview|min|next|object|oct|open|ord|pow|print|property|range|repr|reversed|round|set|setattr|slice|sorted|staticmethod|str|sum|super|tuple|type|vars|zip|__import__)$"))

(call
  function: (attribute attribute: (identifier) @function.method))
(call
  function: (identifier) @function)

; Function definitions

(function_definition
  name: (identifier) @function)

(attribute attribute: (identifier) @property)
(type (identifier) @type)

; Identifier naming conventions (heuristics, then the catch-all)

((identifier) @variable.builtin
 (#match? @variable.builtin "^(self|cls|mcls|metacls)$"))

((identifier) @constant
 (#match? @constant "^[A-Z][A-Z_]*$"))

((identifier) @constructor
 (#match? @constructor "^[A-Z]"))

(identifier) @variable

; Literals

[
  (none)
  (true)
  (false)
] @constant.builtin

[
  (integer)
  (float)
] @number

(comment) @comment

; Strings decompose into their parts (#1466): a whole-node (string) span would
; start before any f-string interpolation inside it and win the first-covering
; lookup, painting the interpolated expression flat. With only the delimiters
; and literal content captured, the expression inside {…} highlights through
; the normal captures above.
(string_start) @string
(string_content) @string
(string_end) @string
(escape_sequence) @escape

; Escaped braces {{ }} are literal text, not interpolation.
(escape_interpolation) @string

; Interpolation braces plus the conversion (!r) and format spec (:.0f), which
; are formatting directives rather than expression code.
(interpolation
  "{" @punctuation.special
  "}" @punctuation.special)
(type_conversion) @punctuation.special
(format_specifier) @punctuation.special

[
  "-"
  "-="
  "!="
  "*"
  "**"
  "**="
  "*="
  "/"
  "//"
  "//="
  "/="
  "&"
  "&="
  "%"
  "%="
  "^"
  "^="
  "+"
  "->"
  "+="
  "<"
  "<<"
  "<<="
  "<="
  "<>"
  "="
  ":="
  "=="
  ">"
  ">="
  ">>"
  ">>="
  "|"
  "|="
  "~"
  "@="
  "and"
  "in"
  "is"
  "not"
  "or"
  "is not"
  "not in"
] @operator

[
  "as"
  "assert"
  "async"
  "await"
  "break"
  "class"
  "continue"
  "def"
  "del"
  "elif"
  "else"
  "except"
  "exec"
  "finally"
  "for"
  "from"
  "global"
  "if"
  "import"
  "lambda"
  "nonlocal"
  "pass"
  "print"
  "raise"
  "return"
  "try"
  "while"
  "with"
  "yield"
  "match"
  "case"
] @keyword
