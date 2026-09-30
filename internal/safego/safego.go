// Package safego starts the goroutines IKE owns (#2836). A panic on a plain
// `go func()` is caught by nobody: not by bubbletea, whose catcher only wraps
// Update, View and the Cmd goroutines it starts itself, and so it kills the
// whole process — the IDE vanishes with the stack printed into an alternate
// screen that is already gone. Go wraps the function in a recover that
// writes a crash report (internal/crashlog) and hands the failure to the
// reporter the app installs (an error notification), so a search worker or a
// language-server reader that panics ends its own goroutine and nothing else.
//
// Every `go` statement under internal/, plugins/ and cmd/ike goes through Go;
// the guard test in this package (goguard_test.go) fails on a bare one that is
// not on its allowlist with a reason.
package safego

import (
	"fmt"
	"sync/atomic"

	"ike/internal/crashlog"
)

// Reporter receives a recovered goroutine panic: the goroutine's name, the
// panic value and the crash-report path ("" when the report could not be
// written). It runs on the panicking goroutine, after the report is on disk.
type Reporter func(name string, value any, path string)

var reporter atomic.Pointer[Reporter]

// SetReporter installs the failure sink (the root model's host.Notify). nil
// clears it; without one the crash report is still written.
func SetReporter(r Reporter) {
	if r == nil {
		reporter.Store(nil)
		return
	}
	reporter.Store(&r)
}

// Go runs fn on a new goroutine under the panic guard. name labels the
// goroutine in the crash report and the notification ("search.rg",
// "lsp.reader"); keep it a stable, content-free identifier.
func Go(name string, fn func()) {
	go func() {
		defer Recover(name)
		fn()
	}()
}

// Recover is the guard itself, for goroutines that cannot be started through
// Go (a goroutine another package starts with a callback of ours): call it
// deferred at the top of the function. It is a no-op without a panic.
func Recover(name string) {
	r := recover()
	if r == nil {
		return
	}
	handle(name, r, crashlog.StackHere())
}

// handle writes the report and notifies. It never panics itself.
func handle(name string, value any, stack []byte) {
	defer func() { _ = recover() }()
	path, err := crashlog.Write(crashlog.Report{
		Where: "goroutine:" + name,
		Value: value,
		Stack: stack,
		Extra: map[string]string{"goroutine": name},
	})
	if err != nil {
		path = ""
	}
	if rp := reporter.Load(); rp != nil {
		(*rp)(name, value, path)
	}
}

// Describe is the notification text for a recovered goroutine panic.
func Describe(name string, value any, path string) string {
	s := fmt.Sprintf("background task %s failed: %s", name, crashlog.Summary(value))
	if path != "" {
		s += " — crash log: " + path
	}
	return s
}
