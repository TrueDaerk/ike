package phpindex

// onchange_test.go covers the debounced content-change callback (#2669) the
// diagnostic refilter hangs off: it fires once the index is warm, again when
// an observed buffer changed a consumer, and never after it was removed.

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"ike/internal/host"
)

// TestOnChangeFiresOnScanAndBufferEdit: installing the callback on a warm
// index notifies once, a buffer edit notifies again, and removing the
// callback stops it.
func TestOnChangeFiresOnScanAndBufferEdit(t *testing.T) {
	x, dir := fixture(t, defaultOpts())

	var calls atomic.Int64
	x.SetOnChange(func() { calls.Add(1) })
	waitFor(t, "the first notification", func() bool { return calls.Load() > 0 })

	// A buffer edit that changes a consumer notifies again.
	before := calls.Load()
	bPath := filepath.Join(dir, "src", "Models", "B.php")
	text, err := os.ReadFile(bPath)
	if err != nil {
		t.Fatal(err)
	}
	x.Observe(host.EditorEvent{Kind: host.EditorChange, Path: bPath,
		Text: string(text) + "\n// touched\n"})
	waitFor(t, "the buffer notification", func() bool { return calls.Load() > before })

	// Removed: a further edit is silent.
	x.SetOnChange(nil)
	waitFor(t, "the settle poll to quiesce", func() bool {
		n := calls.Load()
		time.Sleep(3 * changeSettle)
		return calls.Load() == n
	})
	quiet := calls.Load()
	x.Observe(host.EditorEvent{Kind: host.EditorChange, Path: bPath, Text: string(text)})
	x.Flush()
	time.Sleep(3 * changeSettle)
	if got := calls.Load(); got != quiet {
		t.Fatalf("a removed callback still fired: %d → %d", quiet, got)
	}
}

// TestOnChangeSilentWhileDisabled: with php.trait_index off there is no
// index to change, so nothing is notified.
func TestOnChangeSilentWhileDisabled(t *testing.T) {
	opts := defaultOpts()
	opts.Enabled = false
	x := New(t.TempDir(), opts)
	var calls atomic.Int64
	x.SetOnChange(func() { calls.Add(1) })
	time.Sleep(3 * changeSettle)
	if got := calls.Load(); got != 0 {
		t.Fatalf("a disabled index notified %d times", got)
	}
}
