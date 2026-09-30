package safego

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"ike/internal/crashlog"
)

// TestGoRecoversPanic: a panicking goroutine ends itself, writes a crash
// report naming it and reaches the reporter — the process lives on.
func TestGoRecoversPanic(t *testing.T) {
	dir := t.TempDir()
	crashlog.SetDir(dir)
	crashlog.ResetForTest()
	t.Cleanup(func() { crashlog.SetDir(""); crashlog.ResetForTest(); SetReporter(nil) })

	var mu sync.Mutex
	var gotName, gotPath string
	var gotValue any
	done := make(chan struct{})
	SetReporter(func(name string, value any, path string) {
		mu.Lock()
		gotName, gotValue, gotPath = name, value, path
		mu.Unlock()
		close(done)
	})
	Go("search.worker", func() {
		var m map[string]int
		m["x"] = 1 // nil map write
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the reporter was not called")
	}
	mu.Lock()
	defer mu.Unlock()
	if gotName != "search.worker" || gotValue == nil || gotPath == "" {
		t.Fatalf("reporter got name=%q value=%v path=%q", gotName, gotValue, gotPath)
	}
	body, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{"where:    goroutine:search.worker", "assignment to entry in nil map", "TestGoRecoversPanic"} {
		if !strings.Contains(s, want) {
			t.Errorf("report lacks %q:\n%s", want, s)
		}
	}
	if !strings.Contains(Describe(gotName, gotValue, gotPath), "background task search.worker failed") {
		t.Error("Describe must name the goroutine")
	}
}

// TestGoRunsNormally: no panic, no report, no reporter call.
func TestGoRunsNormally(t *testing.T) {
	dir := t.TempDir()
	crashlog.SetDir(dir)
	t.Cleanup(func() { crashlog.SetDir(""); SetReporter(nil) })
	called := false
	SetReporter(func(string, any, string) { called = true })
	var wg sync.WaitGroup
	wg.Add(1)
	Go("plain", func() { defer wg.Done() })
	wg.Wait()
	if called || len(crashlog.List(dir)) != 0 {
		t.Fatal("a clean goroutine must leave no trace")
	}
}

// TestRecoverDeferred: the guard works as a deferred call in goroutines
// started elsewhere.
func TestRecoverDeferred(t *testing.T) {
	crashlog.SetDir(t.TempDir())
	crashlog.ResetForTest()
	t.Cleanup(func() { crashlog.SetDir(""); crashlog.ResetForTest(); SetReporter(nil) })
	done := make(chan string, 1)
	SetReporter(func(name string, _ any, _ string) { done <- name })
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer Recover("deferred")
		panic("x")
	}()
	wg.Wait()
	if got := <-done; got != "deferred" {
		t.Fatalf("reporter name = %q", got)
	}
}
