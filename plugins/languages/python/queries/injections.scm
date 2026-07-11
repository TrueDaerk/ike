; Embedded-language injections for LSP virtual documents (roadmap 0300).
; Capture names follow the ike convention: fragment.<lang>[.guess] — .guess
; means the Go-side content heuristic decides (SQL keyword leaders), so plain
; strings never become fragments.
(string (string_content) @fragment.sql.guess)
