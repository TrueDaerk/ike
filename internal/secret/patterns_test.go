package secret

import "testing"

// install sets the custom patterns for one test and clears them afterwards —
// the list is a process-wide global, so a leak would decide other tests.
func install(t *testing.T, entries ...string) {
	t.Helper()
	SetKeyPatterns(entries)
	t.Cleanup(func() { SetKeyPatterns(nil) })
}

// TestCustomPatternsMask: a key the built-in tables never heard of masks once
// the user names it, wildcards included.
func TestCustomPatternsMask(t *testing.T) {
	install(t, "MY_LICENCE", "*_LICENSE", "db_pass*")
	for _, k := range []string{"MY_LICENCE", "my_licence", "ACME_LICENSE", "db_passphrase_v2", "DB_PASSAGE"} {
		if !Suspect(k) {
			t.Errorf("Suspect(%q) = false, want true", k)
		}
	}
	// Patterns match the whole key, not a substring of it.
	for _, k := range []string{"MY_LICENCE_ID", "LICENSE", "prefix_db_pass"} {
		if Suspect(k) {
			t.Errorf("Suspect(%q) = true, want false", k)
		}
	}
}

// TestNegativePatternExempts: `-` and `!` clear a key the built-in patterns
// would otherwise mask.
func TestNegativePatternExempts(t *testing.T) {
	install(t, "-PUBLIC_TOKEN", "!SHARED_PASSWORD", "- SPACED_TOKEN")
	for _, k := range []string{"PUBLIC_TOKEN", "public_token", "SHARED_PASSWORD", "SPACED_TOKEN"} {
		if Suspect(k) {
			t.Errorf("Suspect(%q) = true, want false", k)
		}
	}
	// A key no entry matches still follows the built-ins.
	if !Suspect("OTHER_TOKEN") {
		t.Error("an unmatched key must still fall through to the built-in patterns")
	}
}

// TestPatternOrderFirstWins: a specific entry placed before a wildcard decides
// the keys it covers, in both directions.
func TestPatternOrderFirstWins(t *testing.T) {
	install(t, "-ACME_PUBLIC_LICENSE", "*_LICENSE")
	if Suspect("ACME_PUBLIC_LICENSE") {
		t.Error("the earlier exemption must win over the later wildcard")
	}
	if !Suspect("ACME_PRIVATE_LICENSE") {
		t.Error("the wildcard must still mask the keys the exemption misses")
	}

	SetKeyPatterns([]string{"*_TOKEN", "-PUBLIC_TOKEN"})
	if !Suspect("PUBLIC_TOKEN") {
		t.Error("the earlier positive entry must win over the later exemption")
	}
}

// TestPositivePrefixAndBlanks: a `+` prefix is the explicit spelling of a
// positive entry; blanks and a bare sign name no key and are skipped.
func TestPositivePrefixAndBlanks(t *testing.T) {
	install(t, "+HOUSE_PIN", "", "   ", "-", "!")
	if !Suspect("HOUSE_PIN") {
		t.Error("a +-prefixed entry must mask")
	}
	if !Suspect("API_KEY") || Suspect("PORT") {
		t.Error("skipped entries must leave the built-in patterns alone")
	}
}

// TestEmptyListLeavesBuiltinsAlone: the default (no entries) changes nothing.
func TestEmptyListLeavesBuiltinsAlone(t *testing.T) {
	install(t)
	if !Suspect("DB_PASSWORD") || Suspect("API_HOST") {
		t.Error("an empty list must not change the built-in verdicts")
	}
}

// TestSetKeyPatternsReportsChange: the app re-parses open editors only when
// the list actually moved, so a config reload that touched nothing else is
// free.
func TestSetKeyPatternsReportsChange(t *testing.T) {
	install(t, "*_LICENSE")
	if SetKeyPatterns([]string{"*_LICENSE"}) {
		t.Error("re-installing the same list must report no change")
	}
	if !SetKeyPatterns([]string{"*_LICENSE", "-PUBLIC_TOKEN"}) {
		t.Error("a different list must report a change")
	}
}
