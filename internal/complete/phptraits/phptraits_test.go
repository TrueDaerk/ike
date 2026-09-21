package phptraits

// phptraits_test.go covers the source's promises (#2668): what it offers
// after `$this->` and `self::`/`static::` inside a trait body, the partial
// filter, the item shape (declaring type in the detail, doc summary, LSP
// kind), and every case in which it must stay silent — a class body, another
// language, a position that is no member access, and php.trait_index = false.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"ike/internal/complete"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
	"ike/internal/phpindex"

	_ "ike/plugins/languages/php"
)

// fixtureFiles mirrors the #2667 fixture down to the shapes this source is
// about: the trait A whose consumer B declares abc(), the sibling trait C
// with a property, a constant and a static factory, a parent class, and a
// plain class that uses no trait at all.
var fixtureFiles = map[string]string{
	"src/Traits/A.php": `<?php

namespace App\Traits;

trait A
{
    public function run(): void
    {
        $this->abc();
        self::make();
    }
}
`,
	"src/Traits/C.php": `<?php

namespace App\Traits;

trait C
{
    protected $x;

    public static $registry;

    const K = 1;

    /**
     * From C.
     *
     * @return int|null
     */
    public function fromC(array $items = []): ?int
    {
        return null;
    }

    public static function make(): static
    {
        return new static();
    }
}
`,
	"src/Models/B.php": `<?php

namespace App\Models;

use App\Traits\A;
use App\Traits\C;
use App\Base\ParentModel;

class B extends ParentModel
{
    use A, C;

    /** Does the abc thing. */
    public function abc(int $times = 1): string
    {
        return str_repeat('abc', $times);
    }
}
`,
	"src/Base/ParentModel.php": `<?php

namespace App\Base;

abstract class ParentModel
{
    protected int $id = 0;

    public static function find(int $id): static
    {
        return new static();
    }
}
`,
	"src/Models/Plain.php": `<?php

namespace App\Models;

class Plain
{
    public function solo(): void
    {
        $this->abc();
    }
}
`,
}

const traitA = `App\Traits\A`

// fixture writes the project into a temp dir and returns the source over an
// index with the scan finished, plus the project root.
func fixture(t *testing.T) (*Source, *phpindex.Index, string) {
	t.Helper()
	dir := t.TempDir()
	for rel, text := range fixtureFiles {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	x := phpindex.New(dir, phpindex.Options{Enabled: true, ParentDepth: 3, MaxFiles: 1000})
	if !x.Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	for start := time.Now(); !x.ScanDone(); {
		if time.Since(start) > 10*time.Second {
			t.Fatal("scan did not finish")
		}
		time.Sleep(2 * time.Millisecond)
	}
	s := New(x)
	// Every fixture file is an open buffer, so the source finds the line
	// before the cursor the way it would in the editor.
	for rel, text := range fixtureFiles {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: p, Text: text})
	}
	return s, x, dir
}

// at builds a request for the position right behind the first occurrence of
// mark on the first line of path that contains it.
func at(t *testing.T, dir, rel, mark string) complete.Request {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	text, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(text), "\n") {
		if c := strings.Index(line, mark); c >= 0 {
			return complete.Request{Path: p, Line: i, Col: len([]rune(line[:c])) + len([]rune(mark))}
		}
	}
	t.Fatalf("%s: no line containing %q", rel, mark)
	return complete.Request{}
}

func labels(items []ilsp.CompletionItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func has(items []ilsp.CompletionItem, label string) (ilsp.CompletionItem, bool) {
	for _, it := range items {
		if it.Label == label {
			return it, true
		}
	}
	return ilsp.CompletionItem{}, false
}

func complete1(t *testing.T, s *Source, req complete.Request) []ilsp.CompletionItem {
	t.Helper()
	items, err := s.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	return items
}

// TestArrowOffersConsumerAndSiblingMembers is the headline case (#2668):
// inside trait A, `$this->` lists the consumer's abc(), the sibling trait's
// fromC() and its property x — none of which the server can resolve there.
func TestArrowOffersConsumerAndSiblingMembers(t *testing.T) {
	s, _, dir := fixture(t)
	items := complete1(t, s, at(t, dir, "src/Traits/A.php", "$this->"))
	for _, want := range []string{"abc(", "fromC(", "x"} {
		if _, ok := has(items, want); !ok {
			t.Fatalf("$this-> items %v: missing %q", labels(items), want)
		}
	}
	// The declaring type is the point of the detail line.
	abc, _ := has(items, "abc(")
	if !strings.Contains(abc.Detail, "class B") {
		t.Errorf("abc( detail = %q, want it to name class B", abc.Detail)
	}
	if !strings.Contains(abc.Detail, "abc(int $times = 1): string") {
		t.Errorf("abc( detail = %q, want the parameter list", abc.Detail)
	}
	if abc.Doc != "Does the abc thing." {
		t.Errorf("abc( doc = %q", abc.Doc)
	}
	if abc.Kind != protocol.KindMethod {
		t.Errorf("abc( kind = %d, want %d", abc.Kind, protocol.KindMethod)
	}
	fromC, _ := has(items, "fromC(")
	if !strings.Contains(fromC.Detail, "trait C") {
		t.Errorf("fromC( detail = %q, want it to name trait C", fromC.Detail)
	}
	if fromC.Doc != "From C." {
		t.Errorf("fromC( doc = %q", fromC.Doc)
	}
	x, _ := has(items, "x")
	if x.Kind != protocol.KindProperty {
		t.Errorf("x kind = %d, want %d", x.Kind, protocol.KindProperty)
	}
	if !strings.Contains(x.Detail, "trait C") {
		t.Errorf("x detail = %q, want it to name trait C", x.Detail)
	}
	// A constant needs `::`, and the static property is not reachable here.
	for _, unwanted := range []string{"K", "$registry", "registry"} {
		if _, ok := has(items, unwanted); ok {
			t.Errorf("$this-> items %v: %q must need ::", labels(items), unwanted)
		}
	}
	// The parent chain counts into the scope: a static method is offered
	// after `->` because PHP allows the call.
	if _, ok := has(items, "find("); !ok {
		t.Errorf("$this-> items %v: missing the parent's find(", labels(items))
	}
}

// TestStaticOffersConstantsAndStatics guards the `self::` rule: constants and
// static members only.
func TestStaticOffersConstantsAndStatics(t *testing.T) {
	s, _, dir := fixture(t)
	items := complete1(t, s, at(t, dir, "src/Traits/A.php", "self::"))
	for _, want := range []string{"K", "make(", "$registry"} {
		if _, ok := has(items, want); !ok {
			t.Fatalf("self:: items %v: missing %q", labels(items), want)
		}
	}
	for _, unwanted := range []string{"fromC(", "x", "abc("} {
		if _, ok := has(items, unwanted); ok {
			t.Errorf("self:: items %v: %q is not static", labels(items), unwanted)
		}
	}
	k, _ := has(items, "K")
	if k.Kind != protocol.KindConstant {
		t.Errorf("K kind = %d, want %d", k.Kind, protocol.KindConstant)
	}
	if !strings.Contains(k.Detail, "trait C") {
		t.Errorf("K detail = %q, want it to name trait C", k.Detail)
	}
}

// TestPartialPrefixFilters guards the partial-identifier case: `$this->ab`
// narrows to abc(.
func TestPartialPrefixFilters(t *testing.T) {
	s, _, dir := fixture(t)
	items := complete1(t, s, at(t, dir, "src/Traits/A.php", "$this->ab"))
	if got := labels(items); len(got) != 1 || got[0] != "abc(" {
		t.Fatalf("$this->ab items = %v, want [abc(]", got)
	}
}

// TestSilentOutsideTraitBody covers the positions the source must not answer
// in: a class body, a non-PHP request, and a PHP position that is no member
// access at all.
func TestSilentOutsideTraitBody(t *testing.T) {
	s, _, dir := fixture(t)
	if items := complete1(t, s, at(t, dir, "src/Models/Plain.php", "$this->")); len(items) != 0 {
		t.Errorf("class body offered %v, want nothing", labels(items))
	}
	req := at(t, dir, "src/Traits/A.php", "$this->")
	req.Lang = "go"
	if items := complete1(t, s, req); len(items) != 0 {
		t.Errorf("non-PHP request offered %v, want nothing", labels(items))
	}
	if items := complete1(t, s, at(t, dir, "src/Traits/A.php", "public fun")); len(items) != 0 {
		t.Errorf("plain code position offered %v, want nothing", labels(items))
	}
}

// TestInertWhenDisabled guards php.trait_index = false: the index drops its
// content and the source answers nothing.
func TestInertWhenDisabled(t *testing.T) {
	s, x, dir := fixture(t)
	req := at(t, dir, "src/Traits/A.php", "$this->")
	if items := complete1(t, s, req); len(items) == 0 {
		t.Fatal("enabled index offered nothing")
	}
	x.Reconfigure(phpindex.Options{Enabled: false, ParentDepth: 3, MaxFiles: 1000})
	if items := complete1(t, s, req); len(items) != 0 {
		t.Errorf("disabled index offered %v, want nothing", labels(items))
	}
}

// TestNilIndexInert guards the no-cgo shape of the source: a build without a
// PHP grammar leaves an index that never fills, and a source without an index
// at all must answer just as quietly.
func TestNilIndexInert(t *testing.T) {
	s := New(nil)
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: "a.php", Text: "<?php\n$this->\n"})
	items, err := s.Complete(context.Background(), complete.Request{Path: "a.php", Line: 1, Col: 7})
	if err != nil || len(items) != 0 {
		t.Fatalf("nil index: items=%v err=%v", labels(items), err)
	}
}

// TestTelemetryReportsNonEmptyAnswers guards the php.trait.complete op: one
// call per non-empty answer, none for an empty one.
func TestTelemetryReportsNonEmptyAnswers(t *testing.T) {
	s, _, dir := fixture(t)
	var counts []int
	s.SetTelemetry(func(n int) { counts = append(counts, n) })
	items := complete1(t, s, at(t, dir, "src/Traits/A.php", "$this->"))
	complete1(t, s, at(t, dir, "src/Models/Plain.php", "$this->"))
	if len(counts) != 1 {
		t.Fatalf("telemetry calls = %v, want one", counts)
	}
	if counts[0] != len(items) {
		t.Errorf("reported %d items, answered %d", counts[0], len(items))
	}
}

// TestSourceContract pins the interface implementations the engine relies on.
func TestSourceContract(t *testing.T) {
	s := New(nil)
	var _ complete.Source = s
	var _ complete.ContextSource = s
	var _ complete.TriggerSource = s
	var _ complete.EventObserver = s
	if s.Name() != "phptraits" {
		t.Errorf("Name = %q", s.Name())
	}
	if s.Priority() != ilsp.PriorityPHPTraits || s.Priority() >= ilsp.PriorityLSP {
		t.Errorf("Priority = %d, want %d and below the server's %d", s.Priority(), ilsp.PriorityPHPTraits, ilsp.PriorityLSP)
	}
	for _, ch := range []string{">", ":"} {
		if !s.TriggerChar(ch) {
			t.Errorf("TriggerChar(%q) = false", ch)
		}
	}
	for _, ch := range []string{".", "$", "("} {
		if s.TriggerChar(ch) {
			t.Errorf("TriggerChar(%q) = true", ch)
		}
	}
}

// TestItemsAreSortedByPrecedence guards that the popup keeps the index's
// precedence order: the consumer's own member before the traits' and the
// parent's.
func TestItemsAreSortedByPrecedence(t *testing.T) {
	s, _, dir := fixture(t)
	items := complete1(t, s, at(t, dir, "src/Traits/A.php", "$this->"))
	keys := make([]string, len(items))
	for i, it := range items {
		keys[i] = it.SortText
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatalf("sort keys %v are not ascending", keys)
	}
	if items[0].Label != "abc(" {
		t.Errorf("first item = %q, want the consumer's abc(", items[0].Label)
	}
}

// TestVisibleMembersFeedTheSource keeps the source honest about its input:
// everything it offers after `$this->` is a member the index reports for the
// trait.
func TestVisibleMembersFeedTheSource(t *testing.T) {
	s, x, dir := fixture(t)
	known := map[string]bool{}
	for _, m := range x.VisibleMembers(traitA) {
		known[strings.TrimPrefix(m.Name, "$")] = true
	}
	for _, it := range complete1(t, s, at(t, dir, "src/Traits/A.php", "$this->")) {
		name := strings.TrimSuffix(strings.TrimPrefix(it.Label, "$"), "(")
		if !known[name] {
			t.Errorf("offered %q, which VisibleMembers does not report", it.Label)
		}
	}
}
