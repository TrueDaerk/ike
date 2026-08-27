package run

import (
	"strings"
	"testing"
)

// TestEnvRowsSorted: the editor opens on a deterministic, sorted row list —
// the same order EnvSlice hands to the spawned process.
func TestEnvRowsSorted(t *testing.T) {
	rows := EnvRows(map[string]string{"B": "2", "A": "1", "C": ""})
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want 3", rows)
	}
	if rows[0].Key != "A" || rows[1].Key != "B" || rows[2].Key != "C" {
		t.Fatalf("rows not sorted: %+v", rows)
	}
	if rows[2].Value != "" {
		t.Fatalf("an empty value is a legitimate variable: %+v", rows[2])
	}
	if EnvRows(nil) != nil {
		t.Fatal("no environment must render as no rows")
	}
}

// TestValidateEnvKey covers the form's per-row rules: an empty key, a key
// carrying "=" and a key with whitespace are all rejected with a message.
func TestValidateEnvKey(t *testing.T) {
	for _, bad := range []string{"", "   ", "A=B", "A B", "A\tB"} {
		if err := ValidateEnvKey(bad); err == nil {
			t.Fatalf("key %q must be rejected", bad)
		}
	}
	for _, ok := range []string{"PATH", " TRIMMED ", "MY_VAR2"} {
		if err := ValidateEnvKey(ok); err != nil {
			t.Fatalf("key %q must be accepted: %v", ok, err)
		}
	}
}

// TestEnvMapRejectsDuplicates: a duplicate key names itself in the error
// instead of silently overwriting the earlier row.
func TestEnvMapRejectsDuplicates(t *testing.T) {
	_, err := EnvMap([]EnvRow{{Key: "A", Value: "1"}, {Key: " A ", Value: "2"}})
	if err == nil || !strings.Contains(err.Error(), "\"A\"") {
		t.Fatalf("duplicate error = %v, want it to name A", err)
	}
	if _, err := EnvMap([]EnvRow{{Key: "A", Value: "1"}, {Key: "", Value: "2"}}); err == nil {
		t.Fatal("an empty key must be rejected by the set validation too")
	}
	env, err := EnvMap([]EnvRow{{Key: " A ", Value: "1"}, {Key: "B", Value: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if env["A"] != "1" || len(env) != 2 {
		t.Fatalf("env = %+v, want trimmed keys and both rows", env)
	}
	if got, _ := EnvMap(nil); got != nil {
		t.Fatal("no rows must clear the overlay")
	}
}

// TestSetEnvRoundTrip: rows edited in the form persist into the store and
// come back as the same rows — and reach the process as KEY=VALUE pairs.
func TestSetEnvRoundTrip(t *testing.T) {
	redirect(t)
	s := Store{}
	cfg := s.Upsert(Config{Name: "prog", Kind: KindRun, Lang: "fake", File: "prog.fake"})
	if err := cfg.SetEnv([]EnvRow{{Key: "B", Value: "2"}, {Key: "A", Value: "1"}}); err != nil {
		t.Fatal(err)
	}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	loaded := Load()
	got := loaded.ByName("prog")
	if got == nil {
		t.Fatal("config lost")
	}
	rows := EnvRows(got.Env)
	if len(rows) != 2 || rows[0].Key != "A" || rows[1].Value != "2" {
		t.Fatalf("round trip lost the environment: %+v", rows)
	}
	if slice := got.EnvSlice(); len(slice) != 2 || slice[0] != "A=1" || slice[1] != "B=2" {
		t.Fatalf("EnvSlice = %+v, want the edited pairs", slice)
	}

	// An invalid set leaves the stored environment untouched.
	if err := got.SetEnv([]EnvRow{{Key: "A", Value: "x"}, {Key: "A", Value: "y"}}); err == nil {
		t.Fatal("duplicate rows must not be storable")
	}
	if len(got.Env) != 2 {
		t.Fatalf("a rejected set must not clobber the environment: %+v", got.Env)
	}

	// Clearing every row drops the overlay entirely.
	if err := got.SetEnv(nil); err != nil {
		t.Fatal(err)
	}
	if got.Env != nil {
		t.Fatalf("cleared environment = %+v, want nil", got.Env)
	}
}

// TestLastLaunchKind: the store remembers *how* the last configuration ran,
// so the rerun chord repeats a debug launch as a debug launch — across a
// save/load cycle, which is what makes it survive a restart.
func TestLastLaunchKind(t *testing.T) {
	redirect(t)
	s := Store{}
	s.Upsert(Config{Name: "prog", Kind: KindRun, Lang: "fake", File: "prog.fake"})
	s.Touch("prog", KindDebug)
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	reloaded := Load()
	cfg, kind := reloaded.LastLaunch()
	if cfg == nil || cfg.Name != "prog" || kind != KindDebug {
		t.Fatalf("LastLaunch = %+v / %q, want prog / debug", cfg, kind)
	}
	// A plain run overwrites the marker.
	s.Touch("prog", KindRun)
	if _, kind := s.LastLaunch(); kind != KindRun {
		t.Fatalf("kind = %q, want run", kind)
	}
	// A store written before #2173 has no marker: that is a plain run.
	old := Store{Configs: s.Configs, LastUsed: "prog"}
	if _, kind := old.LastLaunch(); kind != KindRun {
		t.Fatalf("legacy kind = %q, want run", kind)
	}
	// Nothing run yet reports no configuration — the chord's toast case.
	empty := Store{}
	if cfg, _ := empty.LastLaunch(); cfg != nil {
		t.Fatalf("LastLaunch = %+v, want nil", cfg)
	}
}
