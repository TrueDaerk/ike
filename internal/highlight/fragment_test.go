package highlight

import "testing"

func TestFragmentCapture(t *testing.T) {
	cases := []struct {
		name  string
		lang  string
		guess bool
		ok    bool
	}{
		{"fragment.sql", "sql", false, true},
		{"fragment.sql.guess", "sql", true, true},
		{"fragment.css", "css", false, true},
		{"fragment.", "", false, false},
		{"fragment", "", false, false},
		{"string.special", "", false, false},
	}
	for _, c := range cases {
		lang, guess, ok := fragmentCapture(c.name)
		if lang != c.lang || guess != c.guess || ok != c.ok {
			t.Errorf("fragmentCapture(%q) = (%q, %v, %v), want (%q, %v, %v)",
				c.name, lang, guess, ok, c.lang, c.guess, c.ok)
		}
	}
}

func TestLooksLikeSQL(t *testing.T) {
	yes := []string{
		"SELECT * FROM users",
		"  select id from t",
		"\n\tINSERT INTO t VALUES (1)",
		"WITH cte AS (SELECT 1) SELECT * FROM cte",
		"DELETE\nFROM t",
		"UPDATE t SET a = 1",
	}
	no := []string{
		"",
		"   ",
		"hello world",
		"SELECTION bias",   // keyword must end at a word boundary
		"WITHDRAW money",   // ditto
		"creates a widget", // lower-case prose, keyword not a prefix token
	}
	for _, s := range yes {
		if !looksLikeSQL(s) {
			t.Errorf("looksLikeSQL(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeSQL(s) {
			t.Errorf("looksLikeSQL(%q) = true, want false", s)
		}
	}
}

func TestGuessFragmentUnknownLang(t *testing.T) {
	if guessFragment("nosuchlang", "SELECT 1") {
		t.Error("unknown guess language must never match")
	}
}

func TestFragmentsUnknownLanguage(t *testing.T) {
	if got := Fragments("nosuchlang", []string{"SELECT 1"}); got != nil {
		t.Errorf("Fragments(nosuchlang) = %v, want nil", got)
	}
}
