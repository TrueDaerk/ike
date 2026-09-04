package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/jqplay"
	"ike/internal/pane"
	"ike/internal/watch"
)

// playwatch_test.go covers the playground's one exception to the snapshot
// principle (#2356): the input is re-read when the file it came from changes
// externally, and only then.

// playWatchApp opens body as a .json file with the playground over it, and
// returns the model together with the file's path so a test can rewrite it
// behind the app's back the way another process would.
func playWatchApp(t *testing.T, body string) (Model, string) {
	t.Helper()
	noDebounce(t)
	m := newSized()
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	return openJQ(t, drainCmd(tm.(Model), cmd)), path
}

// playExternalWrite rewrites path and delivers the watcher event the change would
// produce, end to end through Update.
func playExternalWrite(t *testing.T, m Model, path, body string) Model {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: path})
	return drainCmd(tm.(Model), cmd)
}

// TestJQPlaygroundFollowsExternalChange is the issue's acceptance case: the
// file changes under an open playground and the result describes the new
// content — while the query, the caret and the history position stay put,
// because the input was renewed, not the playground.
func TestJQPlaygroundFollowsExternalChange(t *testing.T) {
	m, path := playWatchApp(t, `{"foo":1}`)
	m = setProgram(m, ".foo")
	if got := m.play.result.Text(); got != "1" {
		t.Fatalf("result = %q, want the original value", got)
	}
	pos, histIdx := m.play.program.Cur, m.play.histIdx

	m = playExternalWrite(t, m, path, `{"foo":42}`)

	if got := m.play.result.Text(); got != "42" {
		t.Fatalf("result = %q, want the value from the changed file", got)
	}
	if m.play.program.Text != ".foo" {
		t.Errorf("program = %q, the query must survive the refresh", m.play.program.Text)
	}
	if m.play.program.Cur != pos || m.play.histIdx != histIdx {
		t.Errorf("caret/history moved: pos %d→%d, histIdx %d→%d", pos, m.play.program.Cur, histIdx, m.play.histIdx)
	}
	if !strings.Contains(m.play.resultEd.Text(), "42") {
		t.Errorf("result buffer = %q, want the refreshed result", m.play.resultEd.Text())
	}
}

// TestJQPlaygroundRefreshIsVisible: the info row says the input was reloaded
// and when — the transient status line alone is gone with the next keystroke,
// and "is this still current?" would be unanswered again.
func TestJQPlaygroundRefreshIsVisible(t *testing.T) {
	m, path := playWatchApp(t, `{"foo":1}`)
	m = setProgram(m, ".foo")
	if !m.play.reloadedAt.IsZero() {
		t.Fatal("an untouched playground must carry no reload stamp")
	}

	m = playExternalWrite(t, m, path, `{"foo":2}`)

	if m.play.reloadedAt.IsZero() {
		t.Fatal("the refresh must stamp its time")
	}
	if !strings.Contains(m.playInfoRow(200), "reloaded") {
		t.Errorf("info row = %q, want the reload marker", m.playInfoRow(200))
	}
	if m.play.status == "" {
		t.Error("the refresh must say so on the status line")
	}
	// The status line survives until the next key, then the stamp carries on.
	tm, _ := m.updatePlaygroundKey(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = tm.(Model)
	if m.play.status != "" {
		t.Errorf("status = %q, a key must clear the transient line", m.play.status)
	}
	if !strings.Contains(m.playInfoRow(200), "reloaded") {
		t.Errorf("info row = %q, the stamp must outlive the status line", m.playInfoRow(200))
	}
}

// TestJQPlaygroundChangeBurstKeepsNewestOnly: several changes in a row do not
// leave competing runs behind — the generation stamp drops every superseded
// parse and evaluation, so only the newest content is on screen.
func TestJQPlaygroundChangeBurstKeepsNewestOnly(t *testing.T) {
	m, path := playWatchApp(t, `{"foo":0}`)
	m = setProgram(m, ".foo")

	m = playExternalWrite(t, m, path, `{"foo":1}`)
	staleGen, stalePGen := m.play.gen, m.play.pgen
	m = playExternalWrite(t, m, path, `{"foo":2}`)
	m = playExternalWrite(t, m, path, `{"foo":3}`)

	if m.play.gen == staleGen || m.play.pgen == stalePGen {
		t.Fatalf("every refresh must advance the stamps (gen %d, pgen %d)", m.play.gen, m.play.pgen)
	}
	if got := m.play.result.Text(); got != "3" {
		t.Fatalf("result = %q, want the newest content", got)
	}

	// The runs and parses the burst abandoned, landing late.
	s := m.play
	m.finishPlayEval(playEvalDoneMsg{st: s, gen: staleGen, res: jqplay.Result{Outputs: []string{"1"}}})
	if got := m.play.result.Text(); got != "3" {
		t.Fatalf("a superseded run installed itself: %q", got)
	}
	m.finishPlayParse(playParseDoneMsg{st: s, gen: stalePGen, err: "stale parse"})
	if m.play.inputErr != "" {
		t.Fatalf("a superseded parse installed itself: %q", m.play.inputErr)
	}
}

// TestJQPlaygroundUnparsableChangeKeepsResult: a change that breaks the
// document leaves the last valid result standing and shows the error — an
// intermediate save of a hand-edited file must not blank the pane, and it
// must not close the playground either.
func TestJQPlaygroundUnparsableChangeKeepsResult(t *testing.T) {
	m, path := playWatchApp(t, `{"foo":1}`)
	m = setProgram(m, ".foo")

	m = playExternalWrite(t, m, path, `{"foo":`)

	if !m.playOpen() {
		t.Fatal("a broken input must not close the playground")
	}
	if m.play.inputErr == "" {
		t.Fatal("the parse error must be reported")
	}
	if got := m.playErrorLine(); got == "" || got != m.play.inputErr {
		t.Errorf("error line = %q, want the input error", got)
	}
	if got := m.play.result.Text(); got != "1" {
		t.Errorf("result = %q, the last valid result must stand", got)
	}
	if !strings.Contains(m.play.resultEd.Text(), "1") {
		t.Errorf("result buffer = %q, want the last valid result", m.play.resultEd.Text())
	}

	// A later repair is picked up again: the mode is not stuck on the error.
	m = playExternalWrite(t, m, path, `{"foo":9}`)
	if m.play.inputErr != "" {
		t.Fatalf("the repaired input still reports %q", m.play.inputErr)
	}
	if got := m.play.result.Text(); got != "9" {
		t.Errorf("result = %q, want the repaired file's value", got)
	}
}

// TestJQPlaygroundClosesWhenSourceFileRemoved: the file is deleted or renamed
// away and the mode ends in a defined state — closed, with a message naming
// the file, rather than querying a document that no longer exists.
func TestJQPlaygroundClosesWhenSourceFileRemoved(t *testing.T) {
	m, path := playWatchApp(t, `{"foo":1}`)
	m = setProgram(m, ".foo")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: path})
	m = drainCmd(tm.(Model), cmd)
	tm, _ = m.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height}) // drain the notification
	m = tm.(Model)

	if m.playOpen() {
		t.Fatal("the playground must not stay open over a removed file")
	}
	var said []string
	for _, h := range m.history {
		said = append(said, h.text)
	}
	if !strings.Contains(strings.Join(said, " "), "was removed") {
		t.Errorf("notifications = %v, want one naming the removal", said)
	}
}

// TestJQPlaygroundKeepsUnsavedBufferAfterRemoval: with unsaved edits the
// buffer is the only copy of the document left, so the mode stays up over
// content that still exists — and says what happened on disk.
func TestJQPlaygroundKeepsUnsavedBufferAfterRemoval(t *testing.T) {
	noDebounce(t)
	m := newSized()
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte(`{"foo":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	m = drainCmd(tm.(Model), cmd)
	// An unsaved edit, still parsable — the buffer now holds a document the
	// file does not.
	if ed := m.activeEditor(); ed != nil {
		ed.RestoreText(`{"foo":1, "unsaved":true}`)
	}
	if ed := m.activeEditor(); ed == nil || !ed.Dirty() {
		t.Fatal("the buffer must be dirty for this case")
	}
	m = openJQ(t, m)
	m = setProgram(m, ".foo")

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	tm, cmd = m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: path})
	m = drainCmd(tm.(Model), cmd)

	if !m.playOpen() {
		t.Fatal("the playground must stay up over the surviving buffer")
	}
	if !m.play.statusWarn || !strings.Contains(m.play.status, "removed") {
		t.Errorf("status = %q (warn %v), want the removal warning", m.play.status, m.play.statusWarn)
	}
	if got := m.play.result.Text(); got != "1" {
		t.Errorf("result = %q, the buffer's result must stand", got)
	}
}

// TestJQPlaygroundIgnoresForeignFileChange: the watcher reports everything it
// watches; a playground follows its own source document and nothing else.
func TestJQPlaygroundIgnoresForeignFileChange(t *testing.T) {
	m, path := playWatchApp(t, `{"foo":1}`)
	m = setProgram(m, ".foo")

	other := filepath.Join(filepath.Dir(path), "other.json")
	if err := os.WriteFile(other, []byte(`{"foo":99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: other})
	m = drainCmd(tm.(Model), cmd)

	if !m.play.reloadedAt.IsZero() {
		t.Fatal("another file's change must not refresh the input")
	}
	if got := m.play.result.Text(); got != "1" {
		t.Errorf("result = %q, want the untouched result", got)
	}
}

// TestJQPlaygroundHTTPResponseIsNotFollowed: an HTTP response is not a file.
// It has no path to watch, and nothing on disk can renew it.
func TestJQPlaygroundHTTPResponseIsNotFollowed(t *testing.T) {
	noDebounce(t)
	m := httpApp(t)
	resp := sampleResponse("one")
	resp.Body = []byte(`{"items":[{"id":7}]}`)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: resp})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)
	m = openJQ(t, m)

	if m.play.srcPath != "" {
		t.Fatalf("srcPath = %q, an HTTP response must not be followed", m.play.srcPath)
	}
}

// TestJQPlaygroundSelectionIsNotFollowed: a selection's character range names
// a different stretch of the file after an edit, so re-reading it would query
// something the user never selected.
func TestJQPlaygroundSelectionIsNotFollowed(t *testing.T) {
	m := playApp(t, "{\"a\":1}\nnot json at all\n")
	// The selection is made on the editor itself: what is under test is the
	// resolved source, not the key that produced the visual mode.
	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("the JSON file must be open in an editor")
	}
	*ed, _ = ed.Update(tea.KeyPressMsg{Code: 'V', Text: "V"})
	m = openJQ(t, m)

	if !strings.Contains(m.play.source, "selection") {
		t.Fatalf("source = %q, want the selection", m.play.source)
	}
	if m.play.srcPath != "" {
		t.Fatalf("srcPath = %q, a selection must not be followed", m.play.srcPath)
	}
}
