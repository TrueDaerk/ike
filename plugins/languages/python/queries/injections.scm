; Embedded-language injections for LSP virtual documents (roadmap 0300).
; Capture names follow the ike convention: fragment.<lang>[.guess] — .guess
; means the Go-side content heuristic decides (SQL keyword leaders, HTML tag
; shape), so plain strings never become fragments.
(string (string_content) @fragment.sql.guess)
; HTML templates in (typically triple-quoted) strings (#1625).
(string (string_content) @fragment.html.guess)
