; Embedded-language injections for highlighting + LSP virtual documents (#995).
; Capture names follow the ike convention: fragment.<lang>[.guess] — .guess
; means the Go-side content heuristic decides (SQL keyword leaders), so plain
; strings never become fragments.
(raw_string_literal (raw_string_literal_content) @fragment.sql.guess)
(interpreted_string_literal (interpreted_string_literal_content) @fragment.sql.guess)
