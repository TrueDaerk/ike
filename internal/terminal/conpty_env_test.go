package terminal

import (
	"testing"
	"unicode/utf16"
)

func TestEnvironmentBlockLayout(t *testing.T) {
	block, err := environmentBlock([]string{"A=1", "PATH=C:\\bin"})
	if err != nil {
		t.Fatal(err)
	}
	want := append(utf16.Encode([]rune("A=1")), 0)
	want = append(want, utf16.Encode([]rune("PATH=C:\\bin"))...)
	want = append(want, 0, 0)
	if len(block) != len(want) {
		t.Fatalf("block length %d, want %d", len(block), len(want))
	}
	for i := range want {
		if block[i] != want[i] {
			t.Fatalf("block[%d] = %#x, want %#x", i, block[i], want[i])
		}
	}
}

func TestEnvironmentBlockEncodesNonBMP(t *testing.T) {
	block, err := environmentBlock([]string{"EMOJI=😀"})
	if err != nil {
		t.Fatal(err)
	}
	// "EMOJI=" is six code units, the emoji a surrogate pair, then NUL NUL.
	if got := len(block); got != 6+2+2 {
		t.Fatalf("block length %d, want 10 (surrogate pair kept)", got)
	}
}

func TestEnvironmentBlockEmpty(t *testing.T) {
	block, err := environmentBlock(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(block) != 2 || block[0] != 0 || block[1] != 0 {
		t.Fatalf("empty env should be the two-NUL block, got %v", block)
	}
}

func TestEnvironmentBlockRejectsNUL(t *testing.T) {
	if _, err := environmentBlock([]string{"A=1\x00B=2"}); err == nil {
		t.Fatal("an entry with an embedded NUL must be rejected")
	}
}
