package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/palette"
	"ike/internal/run"
)

// task_picker.go is the Run Task picker (#1915, run.task): the targets the
// registered task providers discover in the project root — Makefile targets,
// package.json scripts, justfile recipes — listed in a locked palette mode.
// Picking one runs it as an ephemeral run configuration (nothing is written);
// run.taskPromote opens the same list but stores the picked task as a normal
// run configuration in .ike/runconfigs.json instead of running it.

// tasksPrefix selects the picker mode inside the palette; opened locked only,
// so the rune has no user-facing prefix story.
const tasksPrefix = ')'

// TaskPickedMsg runs — or, with Promote, persists — one picked task.
type TaskPickedMsg struct {
	Task    lang.Task
	Promote bool
}

// tasksMode is the palette Mode listing the discovered tasks; the model fills
// entries before each locked open (the runConfigsMode pattern). promote
// mirrors which command opened the picker.
type tasksMode struct {
	entries []lang.Task
	promote bool
}

func newTasksMode() *tasksMode { return &tasksMode{} }

// Prefix implements palette.Mode.
func (t *tasksMode) Prefix() rune { return tasksPrefix }

// Placeholder implements palette.Mode.
func (t *tasksMode) Placeholder() string {
	if t.promote {
		return "Promote task to run configuration…"
	}
	return "Run task…"
}

// Results implements palette.Mode: rows fuzzy-matched over the task label
// ("make: build"), detailing the literal command line.
func (t *tasksMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	for _, e := range t.entries {
		res, ok := fuzzy.Match(query, e.Label())
		if !ok {
			continue
		}
		items = append(items, palette.Item{
			Title:  e.Label(),
			Spans:  res.Positions,
			Score:  res.Score,
			Detail: strings.Join(e.Argv, " "),
			Msg:    TaskPickedMsg{Task: e, Promote: t.promote},
		})
	}
	return items
}

// openTaskPicker opens the palette locked to the tasks mode (run.task /
// run.taskPromote). Nothing to list explains itself instead of showing an
// empty palette.
func (m *Model) openTaskPicker(promote bool) {
	tasks := lang.Tasks(projectRoot())
	if len(tasks) == 0 {
		m.host.Notify(host.Info, "tasks: nothing discovered — no Makefile targets, package.json scripts or justfile recipes here")
		return
	}
	m.tasks.entries = tasks
	m.tasks.promote = promote
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(m.paletteContext(), tasksPrefix)
}

// runPickedTask handles one picked task row. Promote stores the task as a
// run configuration (Upsert folds re-promotions of the same task into one
// entry) without running it; a plain pick launches the task — through the
// stored configuration when one of the same name exists, so a promoted
// task's later edits (narrowed matchers, extra env) apply to picker runs too.
func (m *Model) runPickedTask(msg TaskPickedMsg) tea.Cmd {
	root := projectRoot()
	cfg := run.TaskConfig(msg.Task)
	store := run.Load()
	if msg.Promote {
		store.Upsert(cfg)
		if err := run.Save(store); err != nil {
			m.host.Notify(host.Warn, "tasks: config not saved: "+err.Error())
			return nil
		}
		m.host.Notify(host.Info, "tasks: \""+cfg.Name+"\" saved as a run configuration")
		return nil
	}
	if stored := store.ByName(cfg.Name); stored != nil {
		return m.launchRun(root, store, stored, false)
	}
	c := cfg
	return m.launchRun(root, store, &c, false)
}
