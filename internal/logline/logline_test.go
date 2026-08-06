package logline

import "testing"

func TestParseLogback(t *testing.T) {
	// logback/log4j default-ish: date time [thread] LEVEL logger - msg
	l := Parse("2024-01-02 10:11:12.345 [main] INFO com.example.Foo - started")
	if l.Timestamp != (Range{0, 23}) {
		t.Errorf("timestamp = %+v", l.Timestamp)
	}
	if l.Level != LevelInfo {
		t.Errorf("level = %v", l.Level)
	}
	if l.LevelRange != (Range{31, 35}) {
		t.Errorf("level range = %+v", l.LevelRange)
	}
	if len(l.Names) != 2 || l.Names[0] != (Range{25, 29}) || l.Names[1] != (Range{36, 51}) {
		t.Errorf("names = %+v", l.Names)
	}
}

func TestParseZapConsole(t *testing.T) {
	l := Parse("2024-01-02T10:11:12.345Z\tERROR\thttp.server\trequest failed")
	if l.Timestamp != (Range{0, 24}) {
		t.Errorf("timestamp = %+v", l.Timestamp)
	}
	if l.Level != LevelError || l.LevelRange != (Range{25, 30}) {
		t.Errorf("level = %v %+v", l.Level, l.LevelRange)
	}
	if len(l.Names) != 1 || l.Names[0] != (Range{31, 42}) {
		t.Errorf("names = %+v", l.Names)
	}
}

func TestParseLogfmt(t *testing.T) {
	//        0123456789012345678901234567890123456789
	l := Parse(`time=2024-01-02T10:11:12Z level=warn logger=db msg="slow query"`)
	if l.Timestamp != (Range{5, 25}) {
		t.Errorf("timestamp = %+v", l.Timestamp)
	}
	if l.Level != LevelWarn || l.LevelRange != (Range{32, 36}) {
		t.Errorf("level = %v %+v", l.Level, l.LevelRange)
	}
	if len(l.Names) != 1 || l.Names[0] != (Range{44, 46}) {
		t.Errorf("names = %+v", l.Names)
	}
}

func TestParseSyslog(t *testing.T) {
	l := Parse("Jan  2 10:11:12 myhost sshd[123]: accepted")
	if l.Timestamp != (Range{0, 15}) {
		t.Errorf("timestamp = %+v", l.Timestamp)
	}
	if l.Level != LevelNone {
		t.Errorf("level = %v", l.Level)
	}
}

func TestParseBracketedLevelAndTimestamp(t *testing.T) {
	l := Parse("[2024-01-02 10:11:12,345] [WARN] worker.pool ready")
	if l.Timestamp != (Range{0, 25}) {
		t.Errorf("timestamp = %+v", l.Timestamp)
	}
	if l.Level != LevelWarn || l.LevelRange != (Range{27, 31}) {
		t.Errorf("level = %v %+v", l.Level, l.LevelRange)
	}
	if len(l.Names) != 1 || l.Names[0] != (Range{33, 44}) {
		t.Errorf("names = %+v", l.Names)
	}
}

func TestParseLevelColon(t *testing.T) {
	l := Parse("ERROR: disk full")
	if l.Level != LevelError || l.LevelRange != (Range{0, 5}) {
		t.Errorf("level = %v %+v", l.Level, l.LevelRange)
	}
}

func TestParseSeverityBuckets(t *testing.T) {
	for tok, want := range map[string]Level{
		"TRACE": LevelDebug, "DEBUG": LevelDebug, "FINE": LevelDebug,
		"INFO": LevelInfo, "NOTICE": LevelInfo,
		"WARN": LevelWarn, "WARNING": LevelWarn,
		"ERROR": LevelError, "FATAL": LevelError, "SEVERE": LevelError,
		"PANIC": LevelError,
	} {
		if got := Parse(tok + " x").Level; got != want {
			t.Errorf("%s: level = %v, want %v", tok, got, want)
		}
	}
}

// TestParseFallback: an unrecognized line — a stack-trace frame — yields the
// zero header, so nothing gets colored.
func TestParseFallback(t *testing.T) {
	l := Parse("\tat com.example.Foo.bar(Foo.java:42)")
	if !l.Timestamp.Empty() || l.Level != LevelNone || len(l.Names) != 0 {
		t.Errorf("fallback line parsed as header: %+v", l)
	}
}

// TestParseMessageWordsNotNames: dotted words in the message must not color —
// the first unrecognized token ends the header scan.
func TestParseMessageWordsNotNames(t *testing.T) {
	l := Parse("INFO started listening on example.com:8080")
	if len(l.Names) != 0 {
		t.Errorf("message token taken as name: %+v", l.Names)
	}
}

func TestRainbowStable(t *testing.T) {
	if rainbowCapture("main") != rainbowCapture("main") {
		t.Error("same name must map to the same slot")
	}
}
