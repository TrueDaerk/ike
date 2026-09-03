package nbview

import (
	"os"
	"strings"
	"testing"
)

// TestParseFixture (#2425) pins the nbformat 4 model: both source spellings,
// execution counts, and one of every output type the viewer renders.
func TestParseFixture(t *testing.T) {
	data, err := os.ReadFile(writeFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	nb, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if nb.Lang != "python" || nb.LangExt != "py" {
		t.Fatalf("language = %q/%q, want python/py", nb.Lang, nb.LangExt)
	}
	if nb.Format != "4.5" {
		t.Fatalf("format = %q, want 4.5", nb.Format)
	}
	if len(nb.Cells) != 6 {
		t.Fatalf("cells = %d, want 6", len(nb.Cells))
	}
	// The array spelling joins without extra newlines, the trailing one is cut.
	if got := nb.Cells[0].Source; got != "# Title\n\nSome *prose* about the data." {
		t.Fatalf("markdown source = %q", got)
	}
	// The plain-string spelling is equally legal.
	if got := nb.Cells[2].Source; got != "total = 6 * 7" {
		t.Fatalf("string source = %q", got)
	}
	if got := nb.Cells[1].ExecCount; got != 1 {
		t.Fatalf("exec count = %d, want 1", got)
	}
	// A null execution_count is "never run", not 0-the-count.
	if got := nb.Cells[5].ExecCount; got != 0 {
		t.Fatalf("null exec count = %d, want 0", got)
	}
	streams := nb.Cells[1].Outputs
	if len(streams) != 2 || streams[0].Name != "stdout" || streams[0].Text != "hello" ||
		streams[1].Name != "stderr" || streams[1].Text != "a warning" {
		t.Fatalf("stream outputs = %+v", streams)
	}
	res := nb.Cells[2].Outputs
	if len(res) != 2 || res[0].Text != "42" || res[0].ExecCount != 2 {
		t.Fatalf("execute_result = %+v", res)
	}
	// The HTML output degrades to text and says so.
	if !res[1].FromHTML || !strings.Contains(res[1].Text, "a") || strings.Contains(res[1].Text, "<table") {
		t.Fatalf("html output = %+v", res[1])
	}
	img := nb.Cells[3].Outputs
	if len(img) != 1 || !img[0].HasImage() || img[0].MIME != "image/png" || img[0].ImageExt() != "png" {
		t.Fatalf("image output = %+v", img)
	}
	if !strings.HasPrefix(string(img[0].Image), "\x89PNG") {
		t.Fatal("image output did not decode to PNG bytes")
	}
	e := nb.Cells[4].Outputs
	if len(e) != 1 || e[0].Ename != "ZeroDivisionError" || e[0].Evalue != "division by zero" || len(e[0].Traceback) != 2 {
		t.Fatalf("error output = %+v", e)
	}
}

// TestParseRejectsNonNotebooks (#2425): malformed JSON returns the syntax
// error verbatim and a well-formed non-notebook says what it is missing —
// both are what the pane shows next to the "open as text" hint.
func TestParseRejectsNonNotebooks(t *testing.T) {
	if _, err := Parse([]byte("{ not json")); err == nil {
		t.Fatal("malformed JSON parsed")
	}
	_, err := Parse([]byte(`{"nbformat": 4, "metadata": {}}`))
	if err == nil || !strings.Contains(err.Error(), "cells") {
		t.Fatalf("missing cells: err = %v", err)
	}
	_, err = Parse([]byte(`{"nbformat": 3, "worksheets": [{"cells": []}]}`))
	if err == nil || !strings.Contains(err.Error(), "nbformat 4") {
		t.Fatalf("nbformat 3: err = %v", err)
	}
}

// TestParseFallsBackToKernelspec (#2425): a notebook without language_info
// still highlights, off the kernelspec's language.
func TestParseFallsBackToKernelspec(t *testing.T) {
	nb, err := Parse([]byte(`{"cells": [], "nbformat": 4, "nbformat_minor": 2,
		"metadata": {"kernelspec": {"language": "python"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if nb.Lang != "python" {
		t.Fatalf("lang = %q, want python", nb.Lang)
	}
}

// TestParseDropsUnshowableOutputs (#2425): an output the viewer can render
// nothing of does not become an empty block in the pane.
func TestParseDropsUnshowableOutputs(t *testing.T) {
	nb, err := Parse([]byte(`{"cells": [{"cell_type": "code", "source": "x",
		"outputs": [{"output_type": "display_data", "data": {"application/vnd.custom+json": "{}"}}]}],
		"nbformat": 4, "nbformat_minor": 0, "metadata": {}}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(nb.Cells[0].Outputs); n != 0 {
		t.Fatalf("outputs = %d, want 0", n)
	}
}

// TestHTMLToText (#2425) pins the degradation: tags go, block elements break
// lines, cells become columns, entities decode, style blocks vanish whole.
func TestHTMLToText(t *testing.T) {
	got := htmlToText(`<style>.x{color:red}</style><table><tr><th>name</th><th>n</th></tr>` +
		`<tr><td>a&amp;b</td><td>1</td></tr></table>`)
	want := "\tname\tn\n\ta&b\t1"
	if got != want {
		t.Fatalf("htmlToText = %q, want %q", got, want)
	}
	if got := htmlToText("<p>one</p><p>two</p>"); got != "one\ntwo" {
		t.Fatalf("paragraphs = %q", got)
	}
	if got := htmlToText("a &#65; &lt;b&gt; &unknown;"); got != "a A <b> &unknown;" {
		t.Fatalf("entities = %q", got)
	}
	// An unclosed "<" is content, not markup — malformed input degrades to
	// more text, never to an error.
	if got := htmlToText("3 < 4"); got != "3 < 4" {
		t.Fatalf("bare lt = %q", got)
	}
}
