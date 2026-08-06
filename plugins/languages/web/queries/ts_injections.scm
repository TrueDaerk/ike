; Embedded-language injections for template literals (#1625). Capture names
; follow the ike convention: fragment.<lang>[.guess] — .guess means the
; Go-side content heuristic decides. The text chunks around ${…}
; substitutions are separate string_fragment nodes; the Go side groups the
; chunks of one template (same capture name, same parent) and judges their
; joined text, so on a hit each chunk injects while the substitution
; expressions keep their TypeScript highlighting.
(template_string (string_fragment) @fragment.html.guess)
(template_string (string_fragment) @fragment.sql.guess)

; Regex contexts (#1631): /…/ literals always, plus the first string argument
; of new RegExp(…) and the bare RegExp(…) call. fragment.regex is not a
; registered language — the highlighter routes it to its built-in regex
; mini-grammar.
(regex pattern: (regex_pattern) @fragment.regex)
(new_expression
  constructor: (identifier) @_regexp_ctor
  arguments: (arguments . (string (string_fragment) @fragment.regex))
  (#eq? @_regexp_ctor "RegExp"))
(call_expression
  function: (identifier) @_regexp_call
  arguments: (arguments . (string (string_fragment) @fragment.regex))
  (#eq? @_regexp_call "RegExp"))
