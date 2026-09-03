package nbview

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

// testdata_test.go builds the fixture notebooks the package tests share: an
// nbformat 4 document with a markdown cell, a code cell carrying every output
// type the viewer renders, and an empty cell.

// pngB64 is a tiny valid PNG as base64, the shape a notebook stores an
// image output in.
func pngB64(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// fixtureJSON is the shared notebook document. The cells are, in order:
// markdown, code with a stream output, code with an execute_result and a
// text/html result, code with an image output, code with an error output, and
// an empty code cell.
func fixtureJSON(t *testing.T) string {
	t.Helper()
	return `{
 "cells": [
  {"cell_type": "markdown", "metadata": {}, "source": ["# Title\n", "\n", "Some *prose* about the data.\n"]},
  {"cell_type": "code", "execution_count": 1, "metadata": {},
   "source": ["import sys\n", "print(\"hello\")\n"],
   "outputs": [{"output_type": "stream", "name": "stdout", "text": ["hello\n"]},
               {"output_type": "stream", "name": "stderr", "text": ["a warning\n"]}]},
  {"cell_type": "code", "execution_count": 2, "metadata": {},
   "source": "total = 6 * 7",
   "outputs": [{"output_type": "execute_result", "execution_count": 2,
                "data": {"text/plain": ["42"]}, "metadata": {}},
               {"output_type": "display_data",
                "data": {"text/html": ["<table><tr><th>name</th><th>n</th></tr><tr><td>alpha</td><td>1</td></tr></table>"]}, "metadata": {}}]},
  {"cell_type": "code", "execution_count": 3, "metadata": {},
   "source": ["plot()\n"],
   "outputs": [{"output_type": "display_data", "data": {"image/png": "` + pngB64(t, 8, 4) + `"}, "metadata": {}}]},
  {"cell_type": "code", "execution_count": 4, "metadata": {},
   "source": ["1 / 0\n"],
   "outputs": [{"output_type": "error", "ename": "ZeroDivisionError", "evalue": "division by zero",
                "traceback": ["Traceback (most recent call last):", "\u001b[0;31mZeroDivisionError\u001b[0m: division by zero"]}]},
  {"cell_type": "code", "execution_count": null, "metadata": {}, "source": [], "outputs": []}
 ],
 "metadata": {"kernelspec": {"language": "python", "name": "python3"},
              "language_info": {"name": "python", "file_extension": ".py"}},
 "nbformat": 4,
 "nbformat_minor": 5
}`
}

// writeFixture writes the shared notebook into a temp dir and returns its path.
func writeFixture(t *testing.T) string {
	t.Helper()
	return writeNotebook(t, fixtureJSON(t))
}

// writeNotebook writes arbitrary notebook content into a temp dir.
func writeNotebook(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.ipynb")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// newModel opens a sized viewer over the fixture at path.
func newModel(t *testing.T, path string) Model {
	t.Helper()
	m := New("notebook", path, theme.DefaultPalette())
	m.SetSize(80, 24)
	return m
}

// stripANSI removes styling from a rendered view so tests assert on content.
func stripANSI(s string) string { return ansi.Strip(s) }
