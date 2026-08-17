package run

import (
	"testing"

	"ike/internal/lang"
)

// Task configurations (#1915): the literal argv wins over language synthesis
// and TaskConfig carries the task's identity into the store shape.
func TestArgvLiteralShortCircuit(t *testing.T) {
	cfg := Config{Name: "make: build", Kind: KindRun, Argv: []string{"make", "build"}}
	argv, ok := Argv("/proj", cfg, "")
	if !ok || len(argv) != 2 || argv[0] != "make" || argv[1] != "build" {
		t.Fatalf("Argv = %v ok=%v, want [make build]", argv, ok)
	}
	// The returned slice is a copy — mutating it must not corrupt the config.
	argv[0] = "mutated"
	if cfg.Argv[0] != "make" {
		t.Fatal("Argv must copy the literal command line")
	}
}

func TestTaskConfig(t *testing.T) {
	task := lang.Task{
		Name: "build", Source: "make",
		Argv:     []string{"make", "build"},
		Matchers: []string{"go", "generic"},
	}
	cfg := TaskConfig(task)
	if cfg.Name != "make: build" || cfg.Kind != KindRun {
		t.Fatalf("cfg = %+v", cfg)
	}
	if len(cfg.Argv) != 2 || len(cfg.Matchers) != 2 {
		t.Fatalf("argv/matchers not carried: %+v", cfg)
	}
	// Upsert folds re-promotions of the same task into one stored entry.
	s := Store{}
	s.Upsert(cfg)
	s.Upsert(TaskConfig(task))
	if len(s.Configs) != 1 {
		t.Fatalf("re-promotion must fold, got %d configs", len(s.Configs))
	}
}

func TestStoreRoundTripKeepsTaskFields(t *testing.T) {
	redirect(t)
	s := Store{}
	s.Upsert(Config{Name: "npm: test", Kind: KindRun, Argv: []string{"npm", "run", "test"}, Matchers: []string{"tsc"}})
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	loaded := Load()
	got := loaded.ByName("npm: test")
	if got == nil || len(got.Argv) != 3 || len(got.Matchers) != 1 || got.Matchers[0] != "tsc" {
		t.Fatalf("round trip lost task fields: %+v", got)
	}
}
