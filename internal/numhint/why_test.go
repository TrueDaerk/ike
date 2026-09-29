package numhint

import (
	"testing"
	"time"
)

// why_test.go covers the provenance the hints carry (#1998): which of the
// three rule levels decided a literal, and the pattern or word it fired on.
// The explain popover reports exactly this, so a wrong attribution is a wrong
// explanation.

// TestWhyComment: a comment override reports its own level, the word that
// named the unit and the comment it was read from (#2816).
func TestWhyComment(t *testing.T) {
	SetFieldUnits([]string{"timeout=ms"})
	defer SetFieldUnits(nil)
	hs := Hints([]string{`"timeout": 500,  # seconds`})
	if len(hs) != 1 {
		t.Fatalf("hints = %+v", hs)
	}
	why := hs[0].Why
	if why.Source != SourceComment {
		t.Fatalf("source = %v, want SourceComment", why.Source)
	}
	if why.Pattern != "seconds" || why.Comment != "# seconds" || why.Key != "timeout" {
		t.Fatalf("pattern/comment/key = %q/%q/%q", why.Pattern, why.Comment, why.Key)
	}
	if why.Unit.Kind != UnitDuration || why.Unit.Base != time.Second {
		t.Fatalf("unit = %v", why.Unit)
	}
}

// TestHintInSeesCommentAbove: the explain entry point reads the comment
// stack above the line the way the scan does.
func TestHintInSeesCommentAbove(t *testing.T) {
	SetFieldUnits(nil)
	lines := []string{"a = 1", "// microseconds", "# seconds", "timeout = 500"}
	h, ok := HintIn(lines, 3, 11)
	if !ok || h.Why.Source != SourceComment || h.Why.Comment != "# seconds" || h.Span.Line != 3 {
		t.Fatalf("HintIn = %+v %v, want the # seconds comment", h, ok)
	}
	if h, ok := HintIn([]string{"# seconds", "", "timeout = 90000"}, 2, 11); !ok || h.Why.Source != SourceKeyWord {
		t.Fatalf("HintIn across a blank = %+v %v, want the key word", h, ok)
	}
	if h, ok := HintAt(0, "timeout = 90000", 11); !ok || h.Why.Source != SourceKeyWord {
		t.Fatalf("HintAt = %+v %v", h, ok)
	}
}

func TestWhyKeyWord(t *testing.T) {
	SetFieldUnits(nil)
	hs := LineHints(0, "max_buffer_size: 10485760")
	if len(hs) != 1 {
		t.Fatalf("hints = %+v", hs)
	}
	why := hs[0].Why
	if why.Source != SourceKeyWord {
		t.Fatalf("source = %v, want SourceKeyWord", why.Source)
	}
	if why.Pattern != "size" || why.Key != "max_buffer_size" {
		t.Fatalf("pattern/key = %q/%q", why.Pattern, why.Key)
	}
	if why.Unit.Kind != UnitBytes {
		t.Fatalf("unit = %v", why.Unit)
	}
}

func TestWhyDurationWordCarriesBase(t *testing.T) {
	SetFieldUnits(nil)
	hs := LineHints(0, "flush_interval = 90000")
	if len(hs) != 1 {
		t.Fatalf("hints = %+v", hs)
	}
	why := hs[0].Why
	if why.Source != SourceKeyWord || why.Pattern != "interval" {
		t.Fatalf("source/pattern = %v/%q", why.Source, why.Pattern)
	}
	if why.Unit.Kind != UnitDuration || why.Unit.Base != time.Millisecond {
		t.Fatalf("unit = %v", why.Unit)
	}
	if UnitName(why.Unit) != "ms" {
		t.Fatalf("unit name = %q", UnitName(why.Unit))
	}
}

func TestWhyShape(t *testing.T) {
	SetFieldUnits(nil)
	hs := LineHints(0, "chunk: 1048576")
	if len(hs) != 1 || hs[0].Why.Source != SourceShape {
		t.Fatalf("hints = %+v", hs)
	}
	if hs[0].Why.Shape == "" {
		t.Fatal("shape rule not worded")
	}
	if hs[0].Why.Unit.Kind != UnitBytes {
		t.Fatalf("unit = %v", hs[0].Why.Unit)
	}
}

func TestWhyFieldRuleNamesPattern(t *testing.T) {
	SetFieldUnits([]string{"*_at=timestamp-s"})
	defer SetFieldUnits(nil)
	hs := LineHints(0, "created_at: 1722945600")
	if len(hs) != 1 {
		t.Fatalf("hints = %+v", hs)
	}
	why := hs[0].Why
	if why.Source != SourceFieldRule || why.Pattern != "*_at" {
		t.Fatalf("source/pattern = %v/%q", why.Source, why.Pattern)
	}
	if UnitName(why.Unit) != "timestamp-s" {
		t.Fatalf("unit name = %q", UnitName(why.Unit))
	}
}

// TestWhyFieldRuleClaimsSilentValue (#1685 + #1998): a mapped field that
// renders nothing still reports its rule — that is the case a user most needs
// explained ("why is nothing shown here").
func TestWhyFieldRuleClaimsSilentValue(t *testing.T) {
	SetFieldUnits([]string{"tiny=bytes"})
	defer SetFieldUnits(nil)
	hs := LineHints(0, "tiny = 512")
	if len(hs) != 1 || hs[0].Span.Capture != "" {
		t.Fatalf("hints = %+v", hs)
	}
	if hs[0].Why.Source != SourceFieldRule || hs[0].Why.Unit.Kind != UnitBytes {
		t.Fatalf("why = %+v", hs[0].Why)
	}
}

func TestValueAt(t *testing.T) {
	line := `{"size": 1024, "ttl_ms": 90000}`
	v, ok := ValueAt(line, 10)
	if !ok || v.Text != "1024" || v.Key != "size" {
		t.Fatalf("value = %+v, ok = %v", v, ok)
	}
	// The column one past the token still belongs to it (#1686's widening).
	after, ok := ValueAt(line, 13)
	if !ok || after.Text != "1024" {
		t.Fatalf("adjacent value = %+v, ok = %v", after, ok)
	}
	second, ok := ValueAt(line, 25)
	if !ok || second.Key != "ttl_ms" || second.Text != "90000" {
		t.Fatalf("second value = %+v", second)
	}
	if _, ok := ValueAt("   ", 1); ok {
		t.Fatal("blank line reported a value")
	}
}

func TestHintAt(t *testing.T) {
	SetFieldUnits(nil)
	line := "max_size: 10485760"
	h, ok := HintAt(3, line, 12)
	if !ok {
		t.Fatal("no hint at the column")
	}
	if h.Span.Line != 3 || h.Span.Capture != SizeCapture {
		t.Fatalf("span = %+v", h.Span)
	}
	if _, ok := HintAt(0, line, 2); ok {
		t.Fatal("the key column reported a hint")
	}
}

func TestUnitNameRoundTrip(t *testing.T) {
	for _, name := range []string{"none", "bytes", "timestamp-s", "timestamp-ms", "octal", "hex", "group", "ns", "us", "ms", "s", "min", "h", "d"} {
		u, ok := ParseUnit(name)
		if !ok {
			t.Fatalf("ParseUnit(%q) failed", name)
		}
		if got := UnitName(u); got != name {
			t.Fatalf("UnitName(ParseUnit(%q)) = %q", name, got)
		}
		if u.Label() == "" {
			t.Fatalf("unit %q has no label", name)
		}
	}
}
