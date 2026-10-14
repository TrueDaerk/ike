package editor

import (
	"os"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ansiblevault"
	"ike/internal/editor/buffer"
	"ike/internal/largefile"
	"ike/internal/textenc"
	"ike/internal/undostore"
)

// asyncload.go keeps the open of a big file off the update loop (#2996).
//
// Load used to read, decode and split the file on the Update goroutine: for
// a file of tens of megabytes that is a stall of hundreds of milliseconds to
// seconds during which no frame is drawn and no key is handled. Now a file
// of asyncLoadBytes or more installs an empty, read-only placeholder
// document under its path at once — the tab appears within the frame, the
// status line's large-file slot reads `[loading…]` — and parks a command
// that does the read, the vault decryption, the decode and the buffer build
// on a goroutine. Its result comes back as a LoadMsg the app routes to the
// view by ParseKey, and lands through the very installDoc a synchronous
// load uses: buffer, encoding, line endings, vault state, breakpoints, marks
// and the large-file flag come out exactly as they would have. Smaller
// files keep the synchronous path (LoadSync), so tests and the switch path
// stay simple.
//
// Cancellation is a generation counter behind a per-view pointer, like the
// search scan's (#2734): a newer load, NewFile, a share, or closing the tab
// bumps it; the goroutine returns nil (bubbletea drops it) once stale, and
// a result that still arrives is dropped by the same check on the loop.

// asyncLoadBytes is the size from which Load reads off the loop. A 4 MB
// file reads, decodes and splits in a few milliseconds from the page cache
// and well under a frame from an SSD (the measured 100 MB fixture took
// ~60 ms warm, i.e. ~2.5 ms per 4 MB), so anything below it is cheaper to
// read in place than to schedule. A constant, not a setting: there is no
// user-facing trade-off, only a cost model.
const asyncLoadBytes = 4 << 20

// loadStore is the per-view cancellation generation.
type loadStore struct{ gen atomic.Int64 }

func newLoadStore() *loadStore { return &loadStore{} }

// pendingLoad describes the load the view is waiting for. A caret or scroll
// restore requested meanwhile (a deferred tab's saved framing, #2177) is
// parked here and applied once the text exists.
type pendingLoad struct {
	gen       int64
	path      string
	key       string // the view's ParseKey at load time, the LoadMsg route
	in        loadInputs
	cursor    *buffer.Position
	top, left int
	hasScroll bool
}

// cmd builds the command that reads the pending file off the loop: read,
// then build, each followed by a generation check so a cancelled load
// yields nil (bubbletea drops it) instead of a result nobody wants.
func (p *pendingLoad) cmd(store *loadStore) tea.Cmd {
	gen, path, key, in := p.gen, p.path, p.key, p.in
	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if store.gen.Load() != gen {
			return nil
		}
		if err != nil {
			return LoadMsg{Key: key, Gen: gen, Path: path, Err: err}
		}
		doc, err := buildDoc(data, in)
		if store.gen.Load() != gen {
			return nil
		}
		return LoadMsg{Key: key, Gen: gen, Path: path, Err: err, doc: doc}
	}
}

// LoadMsg is an asynchronous load's result, routed to the view by Key (its
// ParseKey) and matched against the generation it was started under. Err
// is the read or decode failure; the placeholder stays empty then, with the
// error on the ex line.
type LoadMsg struct {
	Key  string
	Gen  int64
	Path string
	Err  error
	doc  loadedDoc
}

// loadedDoc is a file as read and decoded, before it is installed into the
// view — the part of a load that needs no editor state.
type loadedDoc struct {
	buf                              *buffer.Buffer
	info                             textenc.Info
	bytes                            int64 // decoded (plaintext) size, the large-file input
	large                            bool
	hash                             string // undostore.Hash of the on-disk bytes; "" when large
	vault                            bool
	vaultPass, vaultLabel, vaultNote string
}

// loadInputs are the loop-side decisions the off-loop build needs: the
// decode fallback (editorconfig charset, config), the large-file limits and
// the resolved vault password source.
type loadInputs struct {
	fallback  textenc.Encoding
	limits    largefile.Limits
	vaultPass string
	vaultSrc  string
	vaultErr  error
}

// buildDoc turns on-disk bytes into a loadedDoc: vault decryption, decode,
// buffer build, the large-file flag and the undo hash. Pure — it reads no
// editor state — so the async command runs it on its goroutine and LoadSync
// in place.
func buildDoc(data []byte, in loadInputs) (loadedDoc, error) {
	raw := data
	var d loadedDoc
	if ansiblevault.IsVault(data) {
		plain, pass, note := decryptVaultWith(data, in.vaultPass, in.vaultSrc, in.vaultErr)
		if note != "" {
			d.vaultNote = note
		} else {
			d.vault, d.vaultPass, d.vaultLabel = true, pass, ansiblevault.Label(data)
			data = plain
		}
	}
	text, info, err := textenc.Decode(data, in.fallback)
	if err != nil {
		return loadedDoc{}, err
	}
	d.buf = buffer.FromString(text)
	d.info = info
	d.bytes = int64(len(data))
	d.large = in.limits.Exceeded(d.bytes, d.buf.LineCount())
	if !d.large {
		// restoreUndo's hash of the on-disk bytes — the ciphertext for a
		// vault file — computed here so the loop never hashes a big file.
		d.hash = undostore.Hash(raw)
	}
	return d, nil
}

// loadInputsFor gathers the loop-side inputs for path, after the path and
// its editorconfig are resolved on the model.
func (m *Model) loadInputsFor() loadInputs {
	pass, src, err := m.resolveVaultPassword()
	return loadInputs{fallback: m.fallbackEncoding(), limits: m.limits(), vaultPass: pass, vaultSrc: src, vaultErr: err}
}

// Loading reports whether the view waits for an asynchronous load (#2996);
// the status line's large-file slot shows `loading…` meanwhile.
func (m Model) Loading() bool { return m.loading != nil }

// TakeLoadCmd drains the command carrying a pending asynchronous load, the
// way takeSearchCmd drains a background landing: the caller that triggered
// the Load batches it into its returned command. maybeReparse drains it
// too, so a key path that forgot still starts the read on its next update.
func (m *Model) TakeLoadCmd() tea.Cmd {
	c := m.loadCmd
	m.loadCmd = nil
	return c
}

// cancelLoad drops the pending load, if any: the goroutine stops at its
// next check and its result, should it still arrive, is ignored.
func (m *Model) cancelLoad() {
	if m.loading == nil {
		return
	}
	m.loading = nil
	m.loadCmd = nil
	if m.load != nil {
		m.load.gen.Add(1)
	}
}

// loadAsync installs the placeholder for path and parks the command that
// reads it. size is the stat size, which decides the provisional large-file
// flag (the line count is unknown until the read lands).
func (m *Model) loadAsync(path string, size int64) {
	m.cancelLoad()
	prevPath := m.path
	m.path = path
	m.clearLangOverride() // the file name classifies the buffer now (#2033)
	m.resolveEditorconfig()
	in := m.loadInputsFor()
	placeholder := loadedDoc{
		buf:   buffer.New(nil),
		info:  textenc.Info{EOL: textenc.LF, Encoding: textenc.UTF8},
		bytes: size,
		large: in.limits.Exceeded(size, 0),
	}
	m.installDoc(path, prevPath, placeholder)
	// No edit, save or paste may land in the placeholder: the text it would
	// change is still on its way, and a :w would truncate the file to it.
	m.readOnly = true
	m.loading = &pendingLoad{gen: m.load.gen.Load(), path: path, key: m.ParseKey(), in: in}
	m.loadCmd = m.loading.cmd(m.load)
}

// ResumeLoadCmd returns the command for the pending load when the one
// handed out at Load time was dropped: the parked command if it is still
// here, else a fresh one. The app's Init uses it for the CLI targets and
// the restored tabs, whose open ran on the constructor thread where no
// command can be returned — before the program starts, nothing can have
// run the first command, so this never doubles a read. nil when nothing
// is loading.
func (m *Model) ResumeLoadCmd() tea.Cmd {
	if m.loading == nil {
		return nil
	}
	if c := m.TakeLoadCmd(); c != nil {
		return c
	}
	return m.loading.cmd(m.load)
}

// applyLoad lands an asynchronous load's result — only the one the view is
// waiting for: generation and route key must still match, or the result
// describes a file the user has since left. On success the document
// installs exactly as a synchronous load's would, the parked caret /
// framing restore applies, and the parse is scheduled; on failure the
// placeholder stays, writable again, with the error on the ex line.
func (m Model) applyLoad(msg LoadMsg) (Model, tea.Cmd) {
	p := m.loading
	if p == nil || msg.Gen != p.gen || msg.Key != m.ParseKey() {
		return m, nil
	}
	m.loading = nil
	m.readOnly = false
	if msg.Err != nil {
		m.cmdMsg = "E: " + msg.Err.Error()
		return m, nil
	}
	m.installDoc(msg.Path, msg.Path, msg.doc)
	if p.cursor != nil {
		m.SetCursor(p.cursor.Line, p.cursor.Col)
	}
	if p.hasScroll {
		m.SetScroll(p.top, p.left)
	}
	return m, m.parseCmd()
}
