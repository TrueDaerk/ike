package app

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/keydoctor"
	"ike/internal/keymap"
)

// deadbindings.go is the root model's half of the dead-binding report
// (#2161): opening the audit over the live binding table, and persisting a
// rebind the report offered. The audit itself lives in internal/keydoctor;
// the app owns the config write-back, exactly as it owns the probe store.

// openDeadBindings opens the doctor's report phase over the effective
// bindings. The table is the live one, so a rebind applied a moment ago is
// already reflected once its config reload landed.
func (m *Model) openDeadBindings() {
	m.keyDoctor.SetSize(m.width, m.height)
	m.keyDoctor.OpenReport(keymap.DetectTerminalEnv(os.Getenv), m.effectiveBindings())
}

// refreshDeadBindings re-audits an open report after a config reload rebuilt
// the binding table — the rebind just applied is gone from the findings, and
// what is left is judged against the keymap as it now stands. A no-op unless
// the report is the overlay's current phase.
func (m *Model) refreshDeadBindings() {
	if !m.keyDoctor.ReportOpen() {
		return
	}
	m.keyDoctor.RefreshReport(keymap.DetectTerminalEnv(os.Getenv), m.effectiveBindings())
}

// effectiveBindings returns the resolved binding table's bindings, empty
// before the first table build.
func (m *Model) effectiveBindings() []keymap.Binding {
	if m.bindings == nil || m.bindings.Table() == nil {
		return nil
	}
	return m.bindings.Table().Bindings()
}

// applyKeymapRebind writes an accepted suggestion through the ordinary keymap
// customization path: the new chord binds the command at user scope and the
// dead chord unbinds, both before a single reload so the table re-resolves
// atomically. The keys are unqualified (`keymap.bindings.<chord>`) — the same
// spelling the settings keymap page's plain rebind writes — which is why the
// suggestion engine conflict-checks candidates across every context.
func (m *Model) applyKeymapRebind(msg keydoctor.RebindMsg) tea.Cmd {
	if msg.Command == "" || msg.New.Len() == 0 {
		return nil
	}
	opts := m.cfgOpts
	command := msg.Command
	newKey := keymap.BindingConfigKey(keymap.Global, msg.New.String(), false)
	oldKey := keymap.BindingConfigKey(keymap.Global, msg.Old.String(), false)
	dropOld := msg.Old.Len() > 0 && !msg.Old.Equal(msg.New)
	m.host.Notify(host.Info, "keymap doctor: "+command+" rebound to "+msg.New.String())
	return func() tea.Msg {
		var diags []config.Diagnostic
		if err := config.WriteKey(opts, config.UserScope, newKey, command); err != nil {
			diags = append(diags, config.Diagnostic{Field: newKey, Message: err.Error()})
		}
		if dropOld {
			if err := config.WriteKey(opts, config.UserScope, oldKey, ""); err != nil {
				diags = append(diags, config.Diagnostic{Field: oldKey, Message: err.Error()})
			}
		}
		cfg, loadDiags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: cfg, Diags: append(loadDiags, diags...)}
	}
}
