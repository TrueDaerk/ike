; Embedded-language injections for highlighting + LSP virtual documents (#995).
; Capture names follow the ike convention: fragment.<lang>[.guess] — .guess
; means the Go-side content heuristic decides (SQL keyword leaders), so plain
; strings never become fragments.
(string (string_content) @fragment.sql.guess)
(encapsed_string (string_content) @fragment.sql.guess)
(heredoc (heredoc_body) @fragment.sql.guess)
(nowdoc (nowdoc_body) @fragment.sql.guess)
