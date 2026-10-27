package langsystemd

import (
	"fmt"
	"testing"

	"ike/internal/lang"
	_ "ike/plugins/languages/ini"
)

// TestRegistered (#3063): every unit-file extension and every drop-in
// `*.conf` resolves to systemd; any other `*.conf` stays ini.
func TestRegistered(t *testing.T) {
	paths := []string{
		"/etc/systemd/system/foo.service.d/override.conf",
		"foo.timer.d/10-schedule.conf",
		"/usr/lib/systemd/system/user-.slice.d/10-defaults.conf",
	}
	for _, typ := range UnitTypes {
		paths = append(paths, "unit."+typ, "/etc/systemd/system/x@."+typ)
	}
	for _, path := range paths {
		if l, ok := lang.ByPath(path); !ok || l.ID != "systemd" {
			t.Errorf("ByPath(%q) = %q, %v; want systemd", path, l.ID, ok)
		}
	}
	for _, path := range []string{
		"/etc/nginx/nginx.conf",
		"conf.d/site.conf",
		"foo.service.d.bak/override.conf", // not a drop-in directory
		".service.d/override.conf",        // no unit name before the suffix
		"/etc/foo.d/x.conf",
	} {
		if l, ok := lang.ByPath(path); !ok || l.ID != "ini" {
			t.Errorf("ByPath(%q) = %q, %v; want ini", path, l.ID, ok)
		}
	}
	if line, _, ok := lang.Comments("a.service"); !ok || line != "#" {
		t.Errorf("Comments = %q, %v; want #", line, ok)
	}
}

// TestTable (#3063): every epic section is present, no section lists a
// directive twice, every directive has a description and every enum its
// values.
func TestTable(t *testing.T) {
	for _, name := range []string{"Unit", "Install", "Service", "Socket", "Timer", "Path",
		"Mount", "Automount", "Swap", "Slice", "Scope"} {
		if _, ok := LookupSection(name); !ok {
			t.Errorf("section [%s] missing", name)
		}
	}
	for _, s := range Sections {
		seen := map[string]bool{}
		for _, dir := range s.Directives {
			if seen[dir.Name] {
				t.Errorf("[%s] %s listed twice", s.Name, dir.Name)
			}
			seen[dir.Name] = true
			if dir.Doc == "" {
				t.Errorf("[%s] %s: no description", s.Name, dir.Name)
			}
			if (dir.Kind == KindEnum) != (len(dir.Values) > 0) {
				t.Errorf("[%s] %s: kind %d with %d enum values", s.Name, dir.Name, dir.Kind, len(dir.Values))
			}
		}
	}
	// The shared groups reach their sections.
	for _, c := range [][2]string{
		{"Service", "ExecStart"}, {"Service", "KillMode"}, {"Service", "MemoryMax"}, {"Service", "PrivateTmp"},
		{"Socket", "User"}, {"Mount", "KillSignal"}, {"Swap", "CPUWeight"}, {"Slice", "MemoryMax"},
		{"Scope", "KillMode"}, {"Timer", "OnCalendar"}, {"Install", "WantedBy"},
	} {
		if _, ok := Lookup(c[0], c[1]); !ok {
			t.Errorf("Lookup(%s, %s) missing", c[0], c[1])
		}
	}
	if _, ok := Lookup("Timer", "ExecStart"); ok {
		t.Error("[Timer] must not list ExecStart")
	}
}

// styled renders spans as "line:text=capture" for compact assertions.
func styled(lines []string) map[string]bool {
	l, _ := lang.ByID("systemd")
	out := map[string]bool{}
	for _, s := range l.Spans(lines) {
		r := []rune(lines[s.Line])
		out[fmt.Sprintf("%d:%s=%s", s.Line, string(r[s.StartCol:s.EndCol]), s.Capture)] = true
	}
	return out
}

func wantSpans(t *testing.T, got map[string]bool, want ...string) {
	t.Helper()
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing span %s", w)
		}
	}
}

// TestServiceSpans (#3063): a representative .service — sections, known and
// unknown directives, booleans, specifiers, both comment leaders and a line
// continuation with a comment inside it.
func TestServiceSpans(t *testing.T) {
	got := styled([]string{
		"# web app",
		"[Unit]",
		"Description=Web app %i",
		"After=network.target",
		"",
		"[Service]",
		"Type=simple",
		"ExecStart=/usr/bin/app \\",
		"; skipped inside the continuation",
		"    --port 8080",
		"Restart=on-failure",
		"PrivateTmp=yes",
		"NoNewPrivileges=TRUE",
		"Enviroment=TYPO=1",
		"X-Custom=value",
		"Description=wrong section",
		"Price=100%%",
		"",
		"[Install]",
		"WantedBy=multi-user.target",
	})
	wantSpans(t, got,
		"0:# web app=comment",
		"1:[=punctuation", "1:Unit=type", "1:]=punctuation",
		"2:Description=attribute", "2:==punctuation", "2:Web app =string", "2:%i=constant.builtin",
		"3:After=attribute", "3:network.target=string",
		"6:Type=attribute", "6:simple=string",
		"7:ExecStart=attribute", "7:/usr/bin/app=string", "7:\\=escape",
		"8:; skipped inside the continuation=comment",
		"9:--port 8080=string",
		"10:Restart=attribute",
		"11:PrivateTmp=attribute", "11:yes=boolean",
		"12:TRUE=boolean",
		"13:Enviroment=variable", "13:TYPO=1=string",
		"14:X-Custom=attribute",
		"15:Description=variable",
		"16:Price=variable", "16:100=string", "16:%%=constant.builtin",
		"19:WantedBy=attribute",
	)
	// The continuation line is a value, not a directive.
	if got["9:--port 8080=variable"] {
		t.Error("continuation line styled as a directive")
	}
}

// TestTimerSpans (#3063): a representative .timer.
func TestTimerSpans(t *testing.T) {
	got := styled([]string{
		"[Unit]",
		"Description=Nightly backup of %H",
		"",
		"[Timer]",
		"OnCalendar=*-*-* 04:00:00",
		"Persistent=true",
		"RandomizedDelaySec=15min",
		"Unit=backup@%u.service",
		"OnCalender=daily",
		"",
		"[Install]",
		"WantedBy=timers.target",
	})
	wantSpans(t, got,
		"1:%H=constant.builtin",
		"3:Timer=type",
		"4:OnCalendar=attribute", "4:*-*-* 04:00:00=string",
		"5:Persistent=attribute", "5:true=boolean",
		"6:RandomizedDelaySec=attribute", "6:15min=string",
		"7:Unit=attribute", "7:backup@=string", "7:%u=constant.builtin", "7:.service=string",
		"8:OnCalender=variable",
		"11:WantedBy=attribute",
	)
}

// TestSpansNoOverlap (#3063): the structural spans never overlap, so no
// styling depends on first-covering-wins ordering (the secret masks and the
// hints are overlays on top and stay out of the check).
func TestSpansNoOverlap(t *testing.T) {
	lines := []string{"[Service]", "ExecStart=/bin/echo %n %i %%x \\", "  more %h"}
	cover := map[[2]int]bool{}
	for _, s := range systemdStructure(lines) {
		for c := s.StartCol; c < s.EndCol; c++ {
			k := [2]int{s.Line, c}
			if cover[k] {
				t.Fatalf("overlap at line %d col %d", s.Line, c)
			}
			cover[k] = true
		}
	}
}
