package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/run"
	"ike/internal/ui"
)

// runconfig_form.go is the run-configuration form (#2173): the environment
// editor for one stored configuration, opened by run.editConfig through the
// configuration picker. Environment variables used to be reachable only by
// hand-editing .ike/runconfigs.json; here they are rows — add, edit, remove —
// validated on commit (no empty keys, no duplicates) and written back into
// the store, from where every later launch spawns its process with them
// (Config.EnvSlice).
//
// The dialog rides the shell like the other step dialogs (#1718): a list of
// rows, and a two-field row editor reached with enter.

// runFormState is the open form; nil when it is closed.
type runFormState struct {
	cfg  string       // the configuration's name (the store key)
	rows []run.EnvRow // the environment under edit; committed on save
	sel  int
	// editing holds the row editor: field 0 is the key, 1 the value. adding
	// marks a row that only joins rows once it validates, so an abandoned
	// "add" leaves nothing behind.
	editing bool
	adding  bool
	field   int
	key     string
	keyPos  int
	val     string
	valPos  int
	// err is the validation message shown under the rows; cleared by the next
	// accepted key.
	err string
}

// openRunConfigForm opens the form for the stored configuration named name.
// The store is re-read here, so the form always edits the configuration's
// current data rather than the picker's open-time copy.
func (m *Model) openRunConfigForm(name string) {
	store := run.Load()
	cfg := store.ByName(name)
	if cfg == nil {
		m.host.Notify(host.Info, "run: configuration \""+name+"\" is gone")
		return
	}
	m.runForm = &runFormState{cfg: cfg.Name, rows: run.EnvRows(cfg.Env)}
	m.renderRunConfigForm()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// runConfigFormOpen reports whether the shell currently shows the form.
func (m Model) runConfigFormOpen() bool { return m.runForm != nil && m.shell.IsOpen() }

// closeRunConfigForm drops the form state and the shell; pending edits that
// were never saved are discarded (the store was never touched).
func (m *Model) closeRunConfigForm() {
	m.runForm = nil
	m.shell.Close()
}

// renderRunConfigForm (re)fills the shell for the current state.
func (m *Model) renderRunConfigForm() {
	s := m.runForm
	avail := m.width - 30
	if avail < 20 {
		avail = 20
	}
	var lines []string
	add := func(l string) { lines = append(lines, l) }
	add("Environment variables")
	add("")
	if len(s.rows) == 0 && !s.adding {
		add("  (none yet — \"a\" adds the first one)")
	}
	for i, r := range s.rows {
		marker := "    "
		if i == s.sel && !s.editing {
			marker = "  > "
		}
		add(marker + r.Key + "=" + r.Value)
	}
	if s.editing {
		add("")
		key, val := s.key, s.val
		if s.field == 0 {
			key = windowedInput(s.key, s.keyPos, avail)
		} else {
			val = windowedInput(s.val, s.valPos, avail)
		}
		verb := "Edit"
		if s.adding {
			verb = "New"
		}
		add(verb + " variable")
		add("  key:   " + key)
		add("  value: " + val)
		add("")
		add("tab switch field · enter apply · esc cancel row")
	} else {
		add("")
		add("↑↓/jk select · a add · enter edit · d remove · ctrl+s save · esc cancel")
	}
	if s.err != "" {
		lines = append(lines, "", "E: "+s.err)
	}
	body := strings.Join(lines, "\n")
	m.shell.SetContent(ui.ModelContent{
		Heading: "Run Configuration — " + s.cfg,
		Body:    func() string { return body },
	})
}

// updateRunConfigForm consumes every key while the form is open: the row list
// by default, the two-field row editor while a row is being edited.
func (m Model) updateRunConfigForm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.runForm
	if s.editing {
		return m.updateRunConfigRow(msg)
	}
	switch msg.String() {
	case "esc":
		m.closeRunConfigForm()
		return m, nil
	case "ctrl+s":
		return m.saveRunConfigForm()
	case "up", "k":
		if len(s.rows) > 0 {
			s.sel = (s.sel + len(s.rows) - 1) % len(s.rows)
		}
	case "down", "j":
		if len(s.rows) > 0 {
			s.sel = (s.sel + 1) % len(s.rows)
		}
	case "a":
		s.editing, s.adding, s.field = true, true, 0
		s.key, s.keyPos, s.val, s.valPos = "", 0, "", 0
	case "enter":
		if len(s.rows) == 0 {
			return m, nil
		}
		s.editing, s.adding, s.field = true, false, 0
		s.key, s.keyPos = s.rows[s.sel].Key, len(s.rows[s.sel].Key)
		s.val, s.valPos = s.rows[s.sel].Value, len(s.rows[s.sel].Value)
	case "d", "delete":
		m.removeRunConfigRow()
	default:
		return m, nil
	}
	s.err = ""
	m.renderRunConfigForm()
	return m, nil
}

// removeRunConfigRow drops the selected row, keeping the selection inside the
// remaining list.
func (m *Model) removeRunConfigRow() {
	s := m.runForm
	if len(s.rows) == 0 {
		return
	}
	s.rows = append(s.rows[:s.sel], s.rows[s.sel+1:]...)
	if s.sel >= len(s.rows) {
		s.sel = len(s.rows) - 1
	}
	if s.sel < 0 {
		s.sel = 0
	}
}

// updateRunConfigRow drives the two-field row editor: tab switches key and
// value, enter validates and applies the row, esc abandons it.
func (m Model) updateRunConfigRow(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.runForm
	switch msg.Code {
	case tea.KeyEscape:
		s.editing, s.adding, s.err = false, false, ""
		m.renderRunConfigForm()
		return m, nil
	case tea.KeyTab:
		s.field = 1 - s.field
		s.err = ""
		m.renderRunConfigForm()
		return m, nil
	case tea.KeyEnter:
		m.applyRunConfigRow()
		m.renderRunConfigForm()
		return m, nil
	}
	text, pos := s.key, s.keyPos
	if s.field == 1 {
		text, pos = s.val, s.valPos
	}
	if out, ncur, handled, _ := ui.EditKey(msg, text, pos); handled {
		if s.field == 0 {
			s.key, s.keyPos = out, ncur
		} else {
			s.val, s.valPos = out, ncur
		}
		s.err = ""
		m.renderRunConfigForm()
	}
	return m, nil
}

// applyRunConfigRow validates the edited row against the rules in
// internal/run and folds it into the list; a rejected row stays open with the
// reason, so a typo is corrected instead of silently dropped.
func (m *Model) applyRunConfigRow() {
	s := m.runForm
	if err := run.ValidateEnvKey(s.key); err != nil {
		s.err = err.Error()
		s.field = 0
		return
	}
	key := strings.TrimSpace(s.key)
	for i, r := range s.rows {
		if r.Key != key {
			continue
		}
		if s.adding || i != s.sel {
			s.err = "duplicate environment key \"" + key + "\""
			s.field = 0
			return
		}
	}
	if s.adding {
		s.rows = append(s.rows, run.EnvRow{Key: key, Value: s.val})
		s.sel = len(s.rows) - 1
	} else {
		s.rows[s.sel] = run.EnvRow{Key: key, Value: s.val}
	}
	s.editing, s.adding, s.err = false, false, ""
}

// saveRunConfigForm writes the edited environment into the store. The rows
// are validated once more as a set — the list can only hold valid rows, but
// the store is the thing every later launch reads, so it is never handed an
// unchecked map.
func (m Model) saveRunConfigForm() (tea.Model, tea.Cmd) {
	s := m.runForm
	store := run.Load()
	cfg := store.ByName(s.cfg)
	if cfg == nil {
		m.host.Notify(host.Info, "run: configuration \""+s.cfg+"\" is gone")
		m.closeRunConfigForm()
		return m, nil
	}
	if err := cfg.SetEnv(s.rows); err != nil {
		s.err = err.Error()
		m.renderRunConfigForm()
		return m, nil
	}
	if err := run.Save(store); err != nil {
		s.err = "not saved: " + err.Error()
		m.renderRunConfigForm()
		return m, nil
	}
	m.closeRunConfigForm()
	m.host.Notify(host.Info, "run: saved environment for \""+cfg.Name+"\"")
	return m, nil
}

// pasteRunConfigForm inserts a paste into the row editor's focused field
// (#1873); the row list itself has no text input to paste into.
func (m *Model) pasteRunConfigForm(text string) bool {
	s := m.runForm
	if s == nil || !s.editing {
		return false
	}
	if s.field == 0 {
		out, pos, changed := ui.PasteText(s.key, s.keyPos, text)
		if !changed {
			return false
		}
		s.key, s.keyPos = out, pos
	} else {
		out, pos, changed := ui.PasteText(s.val, s.valPos, text)
		if !changed {
			return false
		}
		s.val, s.valPos = out, pos
	}
	m.renderRunConfigForm()
	return true
}
