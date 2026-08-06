; Embedded-language injections for highlighting + LSP virtual documents (#995).
; Capture names follow the ike convention: fragment.<lang>[.guess] — .guess
; means the Go-side content heuristic decides (SQL keyword leaders), so plain
; strings never become fragments.
(raw_string_literal (raw_string_literal_content) @fragment.sql.guess)
(interpreted_string_literal (interpreted_string_literal_content) @fragment.sql.guess)

; Regex call sites (#1631): the first argument of regexp.Compile /
; regexp.MustCompile (and the POSIX variants) highlights with the built-in
; regex mini-grammar — fragment.regex is not a registered language, the
; highlighter routes it to its own tokenizer.
(call_expression
  function: (selector_expression) @_regexp_fn
  arguments: (argument_list . [
    (raw_string_literal (raw_string_literal_content) @fragment.regex)
    (interpreted_string_literal (interpreted_string_literal_content) @fragment.regex)])
  (#match? @_regexp_fn "^regexp\\.(MustCompile|Compile)(POSIX)?$"))
