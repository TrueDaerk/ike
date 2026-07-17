// Package jbimport translates a JetBrains keymap XML export into IKE keymap
// overrides (#677). JetBrains IDEs export keymaps as
//
//	<keymap version="1" name="macOS copy" parent="macOS">
//	  <action id="SaveDocument">
//	    <keyboard-shortcut first-keystroke="meta pressed S"/>
//	  </action>
//	</keymap>
//
// Plan parses such a document, translates every keystroke into an IKE chord
// string (internal/keymap format, logical modifiers: meta→cmd) and maps the
// IntelliJ action ids onto IKE command ids. The result is a set of
// keymap.bindings.* overrides the caller writes at user scope; unmapped
// actions and untranslatable keystrokes are collected, never fatal.
//
// Leaf discipline: this package depends only on internal/keymap and the
// standard library; config write-back stays with the caller.
package jbimport

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"

	"ike/internal/keymap"
)

// xmlKeymap mirrors the JetBrains export document.
type xmlKeymap struct {
	XMLName xml.Name    `xml:"keymap"`
	Name    string      `xml:"name,attr"`
	Parent  string      `xml:"parent,attr"`
	Actions []xmlAction `xml:"action"`
}

type xmlAction struct {
	ID        string        `xml:"id,attr"`
	Shortcuts []xmlShortcut `xml:"keyboard-shortcut"`
	// mouse-shortcut elements are intentionally not modelled: IKE chords are
	// keyboard-only, so they are skipped without diagnostics.
}

type xmlShortcut struct {
	First  string `xml:"first-keystroke,attr"`
	Second string `xml:"second-keystroke,attr"`
}

// Result is one planned import: the overrides to write plus the diagnostics.
type Result struct {
	// Name is the keymap's display name from the export ("" when absent).
	Name string
	// Bind maps chord string → IKE command id: the keymap.bindings.* overrides
	// to write. Chords are canonical (keymap.Chord.String) and logical
	// (cmd stays cmd; platform normalisation happens at table build).
	Bind map[string]string
	// Unbind lists default chords of imported commands the export replaces:
	// chords bound to an imported command in the preset defaults but absent
	// from the export. Writing chord→"" drops them, so the imported chord
	// becomes the command's binding rather than one of several.
	Unbind []string
	// Unmapped lists action ids that carried keyboard shortcuts but have no
	// IKE counterpart, sorted and de-duplicated.
	Unmapped []string
	// Skipped lists keystrokes that could not be translated ("<action>: <keystroke>: <reason>").
	Skipped []string
}

// Summary is the one-line import report for toasts / the settings footer.
func (r *Result) Summary() string {
	s := fmt.Sprintf("imported %d binding(s), unbound %d replaced default(s)", len(r.Bind), len(r.Unbind))
	if n := len(r.Unmapped); n > 0 {
		s += fmt.Sprintf(", %d action(s) unmapped", n)
	}
	if n := len(r.Skipped); n > 0 {
		s += fmt.Sprintf(", %d keystroke(s) skipped", n)
	}
	return s
}

// actionMap maps IntelliJ action ids onto IKE command ids. It covers every
// default-set command (keymap/defaults.go) with a plausible JetBrains
// counterpart; several JetBrains spellings may fold onto one IKE command.
// Actions without an entry land in Result.Unmapped.
var actionMap = map[string]string{
	// Files & saving.
	"SaveDocument": "editor.write",
	"SaveAll":      "editor.saveAll",
	"CloseContent": "editor.closeTab",
	"CloseEditor":  "editor.closeTab",

	// Clipboard & history.
	"$Copy":         "editor.copy",
	"EditorCopy":    "editor.copy",
	"$Cut":          "editor.cut",
	"EditorCut":     "editor.cut",
	"$Paste":        "editor.paste",
	"EditorPaste":   "editor.paste",
	"PasteMultiple": "editor.pasteFromHistory",
	"$Undo":         "editor.undo",
	"$Redo":         "editor.redo",

	// Editing.
	"EditorDuplicate":       "editor.duplicateLine",
	"CommentByLineComment":  "editor.commentLine",
	"CommentByBlockComment": "editor.commentBlock",
	"EditorLineStart":       "editor.lineStart",
	"EditorLineEnd":         "editor.lineEnd",
	"SelectNextOccurrence":  "editor.caret.addNext",
	"SelectAllOccurrences":  "editor.caret.addAll",

	// Find & replace.
	"Find":          "editor.find",
	"Replace":       "editor.replace",
	"FindInPath":    "project.findInPath",
	"ReplaceInPath": "project.replaceInPath",
	"FindNext":      "search.nextMatch",
	"FindPrevious":  "search.prevMatch",

	// Navigation & popups.
	"SearchEverywhere":     "palette.searchEverywhere",
	"GotoAction":           "palette.searchEverywhere",
	"GotoFile":             "project.goToFile",
	"GotoClass":            "project.goToClass",
	"GotoSymbol":           "project.goToClass",
	"RecentFiles":          "palette.recentFiles",
	"ManageRecentProjects": "project.switch",
	"Back":                 "nav.back",
	"Forward":              "nav.forward",
	"NextTab":              "editor.tab.next",
	"PreviousTab":          "editor.tab.prev",
	"ReopenClosedTab":      "editor.tab.reopenClosed",
	"Switcher":             "pane.switcher",

	// Code insight (LSP).
	"GotoDeclaration":      "lsp.definition",
	"FindUsages":           "lsp.references",
	"RenameElement":        "lsp.rename",
	"ReformatCode":         "lsp.format",
	"ShowIntentionActions": "lsp.codeAction",
	"QuickJavaDoc":         "lsp.hover",
	"QuickDocumentation":   "lsp.hover",
	"ParameterInfo":        "lsp.parameterInfo",
	"GotoNextError":        "lsp.nextDiagnostic",
	"GotoPreviousError":    "lsp.prevDiagnostic",
	"CallHierarchy":        "lsp.callHierarchy",

	// Refactors on files.
	"Move": "file.move",

	// Tool windows & views.
	"ActivateProjectToolWindow":  "explorer.toggle",
	"ActivateTerminalToolWindow": "terminal.toggle",
	"ActivateTODOToolWindow":     "todo.list",
	"SelectInProjectView":        "explorer.reveal",
	"ShowSettings":               "settings.open",
	"ToggleDistractionFreeMode":  "view.zenMode",
	"SplitVertically":            "editor.splitViewRight",
	"SplitHorizontally":          "editor.splitViewDown",
	"NewScratchFile":             "scratch.new",

	// Diff viewer.
	"NextDiff":     "diff.nextChange",
	"PreviousDiff": "diff.prevChange",

	// VCS.
	"CheckinProject":      "vcs.commit",
	"Vcs.UpdateProject":   "vcs.updateProject",
	"ChangesView.Revert":  "vcs.revertFile",
	"Git.Branches":        "vcs.branches",
	"Annotate":            "vcs.blameLine",
	"Compare.SameVersion": "vcs.diff",

	// Run & debug.
	"Run":                  "run.file",
	"RunClass":             "run.file",
	"Debug":                "debug.start",
	"DebugClass":           "debug.start",
	"Rerun":                "run.rerun",
	"Stop":                 "debug.stop",
	"ToggleLineBreakpoint": "debug.toggleBreakpoint",
	"StepOver":             "debug.stepOver",
	"StepInto":             "debug.stepInto",
	"StepOut":              "debug.stepOut",
	"Resume":               "debug.continue",
}

// jbKeyName maps JetBrains keystroke key tokens (java.awt.event.KeyEvent VK_
// names, minus the prefix) onto IKE base-key names. Letters, digits and
// F-keys are handled generically in parseKeyToken.
var jbKeyName = map[string]string{
	"ENTER":         "enter",
	"ESCAPE":        "esc",
	"TAB":           "tab",
	"SPACE":         "space",
	"BACK_SPACE":    "backspace",
	"DELETE":        "delete",
	"INSERT":        "insert",
	"HOME":          "home",
	"END":           "end",
	"PAGE_UP":       "pgup",
	"PAGE_DOWN":     "pgdown",
	"UP":            "up",
	"DOWN":          "down",
	"LEFT":          "left",
	"RIGHT":         "right",
	"MINUS":         "-",
	"EQUALS":        "=",
	"COMMA":         ",",
	"SLASH":         "/",
	"BACK_SLASH":    `\`,
	"SEMICOLON":     ";",
	"QUOTE":         "'",
	"BACK_QUOTE":    "`",
	"OPEN_BRACKET":  "left-bracket",
	"CLOSE_BRACKET": "right-bracket",
	// PERIOD is deliberately absent: a "." inside a chord cannot round-trip
	// through the dotted keymap.bindings.<chord> config key.
}

// jbModifier maps JetBrains keystroke modifier tokens onto IKE modifier tokens.
var jbModifier = map[string]string{
	"meta":    "cmd",
	"ctrl":    "ctrl",
	"control": "ctrl",
	"alt":     "alt",
	"shift":   "shift",
}

// ParseKeystroke translates one JetBrains keystroke ("meta pressed S",
// "shift ctrl F7") into a canonical single-step IKE chord string.
func ParseKeystroke(s string) (string, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", fmt.Errorf("empty keystroke")
	}
	var mods []string
	var base string
	for i, f := range fields {
		low := strings.ToLower(f)
		if low == "pressed" || low == "typed" {
			continue
		}
		if m, ok := jbModifier[low]; ok && i < len(fields)-1 {
			mods = append(mods, m)
			continue
		}
		if base != "" {
			return "", fmt.Errorf("two key tokens in %q", s)
		}
		b, err := parseKeyToken(f)
		if err != nil {
			return "", err
		}
		base = b
	}
	if base == "" {
		return "", fmt.Errorf("no key token in %q", s)
	}
	step := strings.Join(append(mods, base), "+")
	// Round-trip through the keymap parser: validates the step and yields the
	// canonical modifier order regardless of the export's ordering.
	k, err := keymap.ParseKey(step)
	if err != nil {
		return "", err
	}
	return k.String(), nil
}

// parseKeyToken maps one key token (the last keystroke field) to an IKE base.
func parseKeyToken(tok string) (string, error) {
	up := strings.ToUpper(tok)
	if len(tok) == 1 {
		c := tok[0]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			return strings.ToLower(tok), nil
		}
	}
	if len(up) >= 2 && up[0] == 'F' && strings.TrimLeft(up[1:], "0123456789") == "" {
		return "f" + up[1:], nil // F1..F24
	}
	if base, ok := jbKeyName[up]; ok {
		return base, nil
	}
	return "", fmt.Errorf("unsupported key token %q", tok)
}

// parseShortcut translates one keyboard-shortcut element (first plus optional
// second keystroke) into a canonical IKE chord string.
func parseShortcut(sc xmlShortcut) (string, error) {
	first, err := ParseKeystroke(sc.First)
	if err != nil {
		return "", err
	}
	chord := first
	if strings.TrimSpace(sc.Second) != "" {
		second, err := ParseKeystroke(sc.Second)
		if err != nil {
			return "", err
		}
		chord += " " + second
	}
	if strings.Contains(chord, ".") {
		// A "." base cannot survive the dotted keymap.bindings.<chord> key.
		return "", fmt.Errorf("chord %q cannot be stored in config", chord)
	}
	return chord, nil
}

// Plan parses a JetBrains keymap XML document and plans the import against
// the given preset defaults: Bind holds the chord→command overrides, Unbind
// the replaced default chords, Unmapped/Skipped the diagnostics.
func Plan(r io.Reader, defaults []keymap.Binding) (*Result, error) {
	var doc xmlKeymap
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("jbimport: not a JetBrains keymap XML: %w", err)
	}
	res := &Result{Name: doc.Name, Bind: map[string]string{}}
	unmapped := map[string]bool{}
	// imported tracks which commands the export rebinds, for the unbind pass.
	imported := map[string]bool{}
	for _, a := range doc.Actions {
		if len(a.Shortcuts) == 0 {
			continue // mouse-only or cleared action: nothing to translate.
		}
		cmd, ok := actionMap[a.ID]
		if !ok {
			unmapped[a.ID] = true
			continue
		}
		for _, sc := range a.Shortcuts {
			chord, err := parseShortcut(sc)
			if err != nil {
				res.Skipped = append(res.Skipped, fmt.Sprintf("%s: %s: %v", a.ID, sc.First, err))
				continue
			}
			res.Bind[chord] = cmd
			imported[cmd] = true
		}
	}
	for id := range unmapped {
		res.Unmapped = append(res.Unmapped, id)
	}
	sort.Strings(res.Unmapped)
	// Unbind pass: default chords of imported commands the export did not
	// keep. The chord may carry other-context bindings too (unbinding drops
	// the whole chord); that matches the keymap page's unbind semantics.
	seen := map[string]bool{}
	for _, b := range defaults {
		cs := b.Chord.String()
		if !imported[b.Command] || seen[cs] {
			continue
		}
		if _, kept := res.Bind[cs]; kept {
			continue
		}
		seen[cs] = true
		res.Unbind = append(res.Unbind, cs)
	}
	sort.Strings(res.Unbind)
	return res, nil
}

// Apply plans the import and writes it through the caller's key writer
// (typically config.WriteKey at user scope): first the unbinds (chord→""),
// then the new bindings (chord→command), in sorted order. The first write
// error aborts and is returned alongside the (partially applied) plan.
func Apply(r io.Reader, defaults []keymap.Binding, write func(key, value string) error) (*Result, error) {
	res, err := Plan(r, defaults)
	if err != nil {
		return nil, err
	}
	for _, chord := range res.Unbind {
		if err := write("keymap.bindings."+chord, ""); err != nil {
			return res, err
		}
	}
	chords := make([]string, 0, len(res.Bind))
	for c := range res.Bind {
		chords = append(chords, c)
	}
	sort.Strings(chords)
	for _, c := range chords {
		if err := write("keymap.bindings."+c, res.Bind[c]); err != nil {
			return res, err
		}
	}
	return res, nil
}
