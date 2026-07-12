package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// --- macro recording & replay (#58) -----------------------------------------

func TestMacroRecordReplay(t *testing.T) {
	m, _ := loaded(t, "abc\nabd\n")
	m = typeKeys(m, "qaxq") // record: delete char under cursor
	if line(m, 0) != "bc" {
		t.Fatalf("recording should apply live, line0=%q", line(m, 0))
	}
	if got := len(m.macros['a']); got != 1 {
		t.Fatalf("macro should hold 1 key (stop q dropped), got %d", got)
	}
	m = typeKeys(m, "j0@a")
	if line(m, 1) != "bd" {
		t.Fatalf("replay should delete char, line1=%q", line(m, 1))
	}
}

func TestMacroRecordsInsertMode(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\n")
	m = send(m, key('q'), key('a'), key('I'), key('X'), special(tea.KeyEscape), key('q'))
	if line(m, 0) != "Xone" {
		t.Fatalf("line0=%q", line(m, 0))
	}
	m = send(m, key('j'), key('@'), key('a'))
	if line(m, 1) != "Xtwo" {
		t.Fatalf("replay across insert mode failed, line1=%q", line(m, 1))
	}
}

func TestMacroReplayCount(t *testing.T) {
	m, _ := loaded(t, "abcdefgh\n")
	m = typeKeys(m, "qaxq") // one delete recorded (and applied)
	m = typeKeys(m, "3@a")  // three more
	if line(m, 0) != "efgh" {
		t.Fatalf("3@a: line0=%q want %q", line(m, 0), "efgh")
	}
}

func TestMacroAtAtRepeatsLast(t *testing.T) {
	m, _ := loaded(t, "abcdefgh\n")
	m = typeKeys(m, "qaxq@a") // 2 deleted
	m = typeKeys(m, "@@")     // 3rd
	if line(m, 0) != "defgh" {
		t.Fatalf("@@: line0=%q", line(m, 0))
	}
	m = typeKeys(m, "2@@") // 4th + 5th
	if line(m, 0) != "fgh" {
		t.Fatalf("2@@: line0=%q", line(m, 0))
	}
}

func TestMacroAtAtWithoutPriorReplay(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m = typeKeys(m, "@@")
	if line(m, 0) != "abc" {
		t.Fatalf("bare @@ must be a no-op, line0=%q", line(m, 0))
	}
}

func TestMacroReplayIsNotReRecorded(t *testing.T) {
	m, _ := loaded(t, "abcdefgh\n")
	m = typeKeys(m, "qaxq")  // macro a: x
	m = typeKeys(m, "qb@aq") // macro b: @a (literal, not the expansion)
	if got := len(m.macros['b']); got != 2 {
		t.Fatalf("macro b should hold the 2 keys @a, got %d", got)
	}
	m = typeKeys(m, "@b")
	if line(m, 0) != "defgh" {
		t.Fatalf("nested replay: line0=%q", line(m, 0))
	}
}

func TestMacroRecursionGuardTerminates(t *testing.T) {
	m, _ := loaded(t, "abcdefgh\n")
	// Recording @a while a is still empty replays nothing but records the
	// keys, so macro a becomes the self-invoking x@a.
	m = typeKeys(m, "qax@aq")
	m = typeKeys(m, "@a") // must terminate via the depth cap
	if line(m, 0) != "" {
		t.Fatalf("recursive macro should empty the line, line0=%q", line(m, 0))
	}
	if m.replayDepth != 0 {
		t.Fatalf("replayDepth must unwind to 0, got %d", m.replayDepth)
	}
}

func TestMacroRecordingStatus(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	if m.Recording() != 0 {
		t.Fatal("idle editor must not report a recording")
	}
	m = typeKeys(m, "qa")
	if m.Recording() != 'a' {
		t.Fatalf("Recording()=%q want 'a'", m.Recording())
	}
	m = typeKeys(m, "q")
	if m.Recording() != 0 {
		t.Fatal("q must stop the recording")
	}
}

func TestMacroInvalidRegisterCancels(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m = send(m, key('q'), key('1'))
	if m.Recording() != 0 {
		t.Fatal("q1 must not start a recording")
	}
	m = send(m, key('q'), special(tea.KeyEscape))
	if m.Recording() != 0 {
		t.Fatal("q esc must not start a recording")
	}
	// The editor still works normally afterwards.
	m = typeKeys(m, "x")
	if line(m, 0) != "bc" {
		t.Fatalf("line0=%q", line(m, 0))
	}
}

func TestMacroEmptyRegisterReplayNoop(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m = typeKeys(m, "@z")
	if line(m, 0) != "abc" {
		t.Fatalf("replaying an empty register must be a no-op, line0=%q", line(m, 0))
	}
}

func TestMacroFindTargetQStaysLiteral(t *testing.T) {
	// While recording, q as an f-target must not stop the recording.
	m, _ := loaded(t, "a quick\n")
	m = typeKeys(m, "qafqxq")
	if m.Recording() != 0 {
		t.Fatal("final q should stop the recording")
	}
	if line(m, 0) != "a uick" {
		t.Fatalf("fq then x: line0=%q", line(m, 0))
	}
	if got := len(m.macros['a']); got != 3 {
		t.Fatalf("macro should hold fqx, got %d keys", got)
	}
}
