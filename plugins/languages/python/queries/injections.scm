; Embedded-language injections for LSP virtual documents (roadmap 0300).
; Capture names follow the ike convention: fragment.<lang>[.guess] — .guess
; means the Go-side content heuristic decides (SQL keyword leaders, HTML tag
; shape), so plain strings never become fragments.
(string (string_content) @fragment.sql.guess)
; HTML templates in (typically triple-quoted) strings (#1625).
(string (string_content) @fragment.html.guess)

; Regex call sites (#1631): the first argument of the re-module matchers
; highlights with the built-in regex mini-grammar (fragment.regex — not a
; registered language, routed to the highlighter's own tokenizer).
(call
  function: (attribute) @_re_fn
  arguments: (argument_list . (string (string_content) @fragment.regex))
  (#match? @_re_fn "^re\\.(compile|match|fullmatch|search|sub|subn|split|findall|finditer)$"))
