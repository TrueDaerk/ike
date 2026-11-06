package format

import (
	"errors"
	"fmt"
	"testing"
)

func TestAsNotice(t *testing.T) {
	if msg, ok := AsNotice(fmt.Errorf("wrapped: %w", Notice("no formatter for json bodies"))); !ok || msg != "no formatter for json bodies" {
		t.Fatalf("wrapped notice not found: %q %v", msg, ok)
	}
	if _, ok := AsNotice(errors.New("boom")); ok {
		t.Fatal("plain error must not be a notice")
	}
	if _, ok := AsNotice(nil); ok {
		t.Fatal("nil must not be a notice")
	}
}
