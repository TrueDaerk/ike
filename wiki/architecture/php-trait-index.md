---
type: concept
title: PHP Trait Index
description: The workspace-wide PHP declaration index (Epic 0520, #2667) — every class-like declaration with its members, the trait-use / extends / implements edges between them, and the consumer scope a `$this` inside a trait body resolves against where Intelephense is blind. Built on the shared per-language project walk, kept fresh from buffer edits and watcher events, configured by the [php] section (Settings → PHP).
resource: internal/phpindex
tags: [architecture, php, traits, index, completion, navigation, hover, references, lsp]
timestamp: 2026-09-21T18:00:00Z
---

# PHP Trait Index

Epic 0520's foundation. In a large legacy PHP code base traits are
horizontal code sharing: trait `A` and trait `C` are always used together by
class `B`; `A` calls `$this->abc()` declared in `B` and members declared in
`C`. Intelephense resolves `$this` inside a trait body as *the trait itself*,
so inside `A` every such member is unknown: no completion, an
undefined-member diagnostic per access, nothing to navigate to. Annotating
hundreds of traits with `@mixin` is not an option, so IKE builds its own
**PHP declaration index** and lets the trait features (completion #2668,
diagnostics #2669, navigation #2670, references #2671, rename #2672, status
#2673) complement the server with it. Everything built on the index is
additive: where the server answers, its answer wins.

The index is **declarations and edges, not types**. It knows which
declarations exist, what they extend / implement / use and which members
they declare; it never infers what `$foo` is. `$foo->bar()` on an arbitrary
variable stays with the server.

## Data model (`internal/phpindex`)

One file extracts to a list of **declarations** (`Decl`): `class`,
`abstract`/`final class`, `trait`, `interface` and `enum`, each with its
short name, resolved fully qualified name (FQN), `Extends`, `Implements`,
the traits it `use`s inside its body, and its **members** (`Member`):

- methods — name, `static`, visibility, the parameter list text, the return
  type text and the docblock's first prose line;
- properties — `$name` with type and visibility, promoted constructor
  parameters included;
- class constants and enum cases;
- **trait-use aliases**: `use X { foo as bar; }` makes `bar` a method member
  of the consumer (`AliasOf` = `foo`); an `insteadof` rule and a
  visibility-only `as` add no member.

Every declaration and member carries its file path plus two ranges in
editor coordinates (0-based line, rune column, exclusive end): the whole
node and the identifier. `Member.Signature()` renders the hover/detail form
(`public static function find(int $id): static`, `protected $x`, `const K`).

**Name resolution happens at extraction time.** A written name becomes an
FQN from the file's namespace (braced and unbraced forms, the import table
reset per namespace), its `use` imports including aliases and grouped
`use A\{B, C as D}` forms, `namespace\X` and a leading `\`. A name the walk
cannot qualify — legacy code without namespaces or imports — keeps its
written form; the snapshot then matches it by **short name** against every
declaration of that name, so `class OldConsumer { use LegacyTrait; }` still
reaches `Legacy\Support\LegacyTrait`. Lookups are case-insensitive on class
names, exact on member names.

The extractor reads the **syntax tree**, not the highlight captures:
`highlight.SyntaxTree` (new with #2667) parses once through the PHP grammar
and hands back a pure-Go snapshot of the named nodes with kinds, field
names and rune-column ranges, closed before returning and safe to keep.
Without cgo the snapshot is nil: the index stays empty, `Stats()` reports
`Unavailable`, and nothing panics.

## Storage and freshness

Storage is the shared per-language project walk
([completion engine](completion.md), `internal/complete/langindex`): one
scan of `*.php` under the project root — a PHP-only path classifier keeps
the walk from reading other languages' files for embedded PHP — with the
walk's skip list (dot directories, `node_modules`, `vendor` unless
`php.index.include_vendor` keeps it), a 512 KiB per-file cap and the
`php.index.max_files` cap; a scan that hits the cap is reported truncated.
The walk records its duration and a **content generation** that every
finished scan and re-extraction bumps.

Freshness has two paths, both off the Update goroutine:

- **Open buffers** (`complete.EventObserver`): the completion engine
  forwards every editor change event to the index through its observer
  registration (`Engine.RegisterObserver`, also new — the index is not a
  completion source and takes part in no dispatch). A PHP buffer's text is
  stashed and a 150 ms debounce re-extracts it on the timer's goroutine; the
  buffer's extraction overrides the on-disk file of the same path until a
  large-file event drops it. `Flush()` runs the pending extraction now for
  callers already off the UI goroutine (and tests).
- **Disk** (`complete.FileObserver`): watcher events reach
  `Engine.NotifyFileChanged` from `routeWatchEvent`; a PHP path queues
  behind the walk's single worker and re-extracts, a removed file drops
  out, other paths are ignored without queueing.

Queries read a **snapshot** derived lazily when the generation, a buffer
or the parent depth changed: declarations by FQN, by short name and by
file, the reverse trait-use edges (`users`: trait → the declarations using
it directly) and the files referring to each declaration.

## Scope rules

For a trait `T` the **consumer scope** is the union, in precedence order,
of the members of

1. **consumers** — every class, enum or trait using `T`, directly or
   through other traits (breadth-first over the reverse edges, a visited set
   makes `trait A { use C; } trait C { use A; }` terminate);
2. `T` itself;
3. **sibling traits** — every other trait those consumers use, transitively;
4. the consumers' **parent chains** — `extends` followed up to
   `php.index.parent_depth` levels (default 3; an undeclared parent or a
   cycle stops the chain), each parent's own traits included.

`VisibleMembers(T)` deduplicates that union by (kind, name), nearest first:
a consumer's own method wins over the trait's, the trait's over a parent's,
mirroring PHP's precedence. `Lookup(T, kind, name)` returns *every*
declaration of a member in the scope (several when consumers declare it
independently); a property matches with or without its `$`.

The rest of the query API, all pure on the snapshot: `ScopeAt(path, pos)`
is the innermost declaration around a position (its `IsTrait` decides
whether a trait feature applies); `ConsumersOf`, `SiblingTraitsOf`,
`ParentChain(fqn, depth)`; `DeclarationsNamed(name)` by FQN or short name;
`FilesReferencing(fqn)` — the files importing, extending, implementing or
using the declaration, the source of a references search; `Stats()` — files,
declarations, edges, scan duration, truncated, scanning, enabled,
unavailable.

## Settings (`[php]`, Settings → PHP)

| key | default | effect |
| --- | --- | --- |
| `php.trait_index` | `true` | master switch for the index and every feature of the epic; off drops the index (buffers included) at once, on rebuilds and rescans it |
| `php.index.parent_depth` | `3` (0–10) | parent levels of the consumers that contribute to the scope; applies without a rescan |
| `php.index.include_vendor` | `false` | read `vendor/` too (the server already covers it); rescans |
| `php.index.max_files` | `20000` (min 100) | walk cap; rescans, and a truncated scan is flagged in `Stats()` |

The loader clamps the ranges with a diagnostic; the form rejects a
non-number and clamps out-of-range input with a notice. Reloads reach the
index through `reloadConfig` → `Reconfigure`.

## Completion (#2668)

`internal/complete/phptraits` is the index's completion source (name
`phptraits`, priority `lsp.PriorityPHPTraits` = 60): inside a trait body it
offers the consumer scope's members after `$this->`, `self::` and `static::`,
which the server cannot resolve there. It is a plain `complete.Source` on the
app's engine — it keeps no index of its own, only the observed buffer text it
reads the line before the cursor from, and asks `ScopeAt` + `VisibleMembers`
once the cheap checks (language `php`, the access syntax before the cursor,
`php.trait_index`) have passed.

The access rule lives in one function: `$this->` reaches every method (static
ones too — PHP allows `$this->staticMethod()`) and the non-static properties;
`self::` / `static::` reach the static members plus constants and enum cases.
Methods are offered as `name(`, properties bare after `->` and with their `$`
after `::`; `detail` names the declaring type (`abc(): string  —  class B`),
`documentation` the docblock summary, and the item kind maps to LSP
Method / Property / Constant / EnumMember. In the fixture project,
`$this->` inside trait `A` therefore lists `abc(` (from the consumer `B`),
`fromC(` and `x` (from the sibling trait `C`) and the parent chain's `find(`;
`self::` lists `K` and the static members.

Because the priority sits below the server's, the editor's per-insert-text
merge keeps the **server's** item for a member both offer — the source only
adds what Intelephense cannot see. A non-empty answer records the telemetry op
`php.trait.complete` with its item count. With `php.trait_index = false`, and
in a build without the PHP grammar, the source answers nothing. See
[completion](completion.md) § PHP trait members for the engine side.

## Diagnostics (#2669)

Because the server resolves `$this` inside a trait as the trait itself,
every `$this->abc()` whose `abc` lives on a consumer comes back as an
undefined member. The rule list
[`lsp.diagnostics_ignore`](lsp.md#data-flow) cannot fix that: it
matches per code and message, so a rule wide enough to hide the access
inside the trait hides a genuine typo everywhere else too. The index makes
the suppression **position-aware** instead.

Every published set already passes one shaping funnel — `applyDiagnostics` →
`filterDiags` (`internal/app/diag_ignore.go`): the ignore rules, then the
trait pass (`internal/app/diag_trait.go`), then the severity remap. The
trait pass drops a diagnostic only when **all three** hold:

1. it is an Intelephense **undefined-member** diagnostic — one of the codes
   `P1013` (method), `P1014` (property), `P1012` (class constant), with the
   message naming that kind of member in quotes as a secondary check, so a
   code reused by a future server version cannot silently widen the pass
   (`internal/lsp/undefmember.go` — the one place the server's vocabulary
   lives);
2. its range lies inside a **trait body** — `ScopeAt(path, pos)` with
   `IsTrait()`;
3. the member the message names **resolves** in that trait's consumer
   scope — `Lookup(traitFQN, kind, name)`.

Everything else passes unchanged: `$this->nope()` inside the trait, the same
code inside the consumer class, another code inside the trait. Genuinely
undefined members stay red. Positions need no conversion — `ConvertDiagnostics`
has already mapped the server's LSP positions to editor coordinates (rune
columns) through the negotiated encoding, which is what the index stores.

**Freshness.** The index notifies the app whenever its content generation
moved (`SetOnChange`, debounced by 250 ms and armed by the events that can
change the content — so one keystroke never refilters the world); the app
answers with `PHPIndexChangedMsg` → `refilterDiagnostics`, which re-runs the
funnel over every cached raw set. A marker therefore disappears once the
initial scan is warm and **comes back** when a re-extract takes the member
off the consumer. With `php.trait_index = false` the pass drops nothing.

**Reporting.** Suppressed diagnostics are counted per path, apart from the
rule-ignored ones, and the Problems panel header names the project-wide
total — `12 errors · 3 warnings · 2 resolved via trait consumers` — since
they are never rows. Telemetry records the op `php.trait.diag_suppressed`
with the count per `applyDiagnostics` call, only when it is greater than
zero.

## Navigation (#2670)

Go-to-definition, peek definition and hover on `$this->abc()` inside a trait
return **nothing** from Intelephense whenever `abc` lives on a consumer — the
bridge could only answer "no definition found under the cursor". The index
fills that gap as a **fallback**, consulted strictly *after* the server:

| seam (`plugins/lsp/bridge.go`) | when the index is asked |
| --- | --- |
| `definitionRequest` (go-to **and** peek) | the server answered **zero** locations, or there is no manager to ask at all |
| `requestHover` | the server answered **null**/empty hover, or there is no manager |

That ordering is the whole design. The pre-server family of
[local providers](lsp.md#layers) (`internal/lsp/localdef.go`) is *first*
claim wins and would bypass the server — exactly wrong here, where the server
is right everywhere except inside a trait body. So this fallback is not a
local provider; a server answer is never replaced.

**The seam.** The bridge is a plugin and may not import `internal/phpindex`,
so the index is reached through the host the way configuration is:
`host.TraitIndex` (`internal/host/traitindex.go`) is a one-method read-only
view the app registers with `Host.SetTraitIndex`, and the bridge asks for it
with `h.TraitIndex()` — no package-level global. Its single question is
*"which members does the trait's consumer scope declare under this
position?"*, answered with flat `host.TraitMember` values (name, signature,
docblock summary, declaring FQN/kind/short name, and the jump target). Every
gate lives behind the seam, in `phpindex.HostView` (`hostview.go`), cheapest
first: the buffer must be PHP, `php.trait_index` must be on, `ScopeAt` must
land in a **trait body**, and the syntax tree must name a member access
there. Only then does `LookupAccess` run.

**Symbol extraction is tree-based, not regex** (`phpindex.MemberAccessAt`,
`access.go`). The parse walks from the innermost node containing the position
up to the nearest member access, so `$this->abc(`, `$this->abc`, `self::K`,
`self::$x` and `static::make()` all resolve, and `$this->outer($this->inner())`
resolves the *inner* call when the cursor sits on it. Only `$this`, `self`
and `static` receivers claim — `$other->abc()` and `parent::gone()` stay the
server's business — and standing **on the receiver** claims nothing, since
`$this` inside a trait names the trait. A `::` access may name a constant or
an enum case and an arrow access a property or a method, so `Access.Kinds`
carries the candidate kinds and the first that resolves wins.

**Delivery matches a server answer exactly** (`plugins/lsp/traitfallback.go`):
one hit sends the same `DefinitionMsg` (or `PeekDefinitionMsg` for peek) the
server path sends, so the app's navigation history, pane dedupe and open
funnel all apply unchanged; **several hits** — the member declared
independently on two consumers — send the same `DefinitionCandidatesMsg` the
[multi-target picker](lsp.md#data-flow) opens for multiple server locations,
each row previewing the signature and its declaring type, with `Peek` carried
through. Nothing resolved means the function reports "not handled" and the
existing #858 notice goes out as before.

**Hover** renders a markdown card per declaration: the signature in a `php`
fence, the declaring type (`class B — App\Models\B`), the docblock summary,
and the footer `resolved via trait consumer B`. Several declarations are
listed, rule-separated. Hover **inside the consumer class** is untouched:
the server resolves the member there, so its answer wins and the index is
never consulted.

Telemetry records `php.trait.definition` and `php.trait.hover` with the
declaration count, only for non-empty answers — a server answer records
nothing, because the index was never asked. With `php.trait_index = false`,
without a registered index and in a build without the PHP grammar the whole
fallback is inert. See [lsp](lsp.md#layers) § definition / hover for the
bridge side.

## References (#2671)

Find usages is blind in **both** directions around traits. Inside trait `A`,
`$this->abc()` resolves to nothing, so the server reports no usages at all;
on `abc()` declared in class `B`, the server lists the declaration and the
calls inside `B` but never the calls inside the traits `B` consumes — which
is what makes a server-side rename unsafe (#2672 builds on this). The index
answers both sides through a **reference scanner** and a **merge** in the
bridge, after the server answered, never in front of it.

**The scanner** (`internal/phpindex/references.go`) is independent from the
bridge so the rename issue can reuse it. `References(member)` returns
`Location` rows — the member identifier's range, the declaring type, whether
that type is a trait, whether the row is the declaration itself — for the
member's **scope**: the declaring type, its traits transitively, its
subclasses within `php.index.parent_depth` (the snapshot's reverse `extends`
edges), and for a trait member every consumer and sibling trait, each with
their trait closures and subclasses. It parses every file of that scope and
matches accesses on the **syntax tree**, never by text: the same
classification `MemberAccessAt` uses (`member_call_expression`,
`member_access_expression`, `scoped_call_expression`,
`scoped_property_access_expression`, `class_constant_access_expression`) on
a `$this` / `self` / `static` receiver, with the exact member name, inside a
declaration belonging to the scope. A same-named member on an unrelated
class, `$other->abc()`, a property-shaped `$this->abc` for the method `abc`,
a string or a comment never match. Anonymous classes are skipped (their
`$this` is their own). A trait-use alias `use X { foo as bar; }` makes `bar`
a name of `X::foo`: usages under either name are found from either member,
and the alias clause counts as a declaration under both names. An open
buffer is scanned in its **observed text** (the same buffer override the
extractor uses), not its on-disk copy. `ReferencesIn(member, path)` is the
same restricted to one file — the occurrence-highlight case — and
`MembersAt(path, text, pos)` resolves the member a position names in *any*
class-like body: the declaration identifier under the cursor, or a
`$this` / `self` / `static` access looked up in the enclosing declaration's
scope (the consumer scope inside a trait; the type, its traits and its parent
chain inside a class or enum).

**The seam** extends `host.TraitIndex` with one more question,
`TraitReferencesAt(op, path, lines, line, col, serverHits)`, answered by
`phpindex.HostView` (`hostview_refs.go`) behind the same gates (PHP buffer,
`php.trait_index` on, a class-like body) plus the rule that decides the
direction:

| server answer | position | what the index adds |
| --- | --- | --- |
| **empty** | inside a **trait body** | the member's whole scope: its declaration(s), every access in the declaring type, its traits, its subclasses, its consumers and sibling traits |
| **empty** | anywhere else | nothing — an empty answer means what it says |
| **non-empty** | any | only the rows **inside trait bodies** (the server reported the consumer side itself) |

**The merge** (`plugins/lsp/traitrefs.go`, `mergeTraitReferences`) runs
after every references request — `findReferences` (the palette list and the
at-definition usages, which drop declaration rows), `findUsages` (the
[Usages pane](usages.md)) and the no-server case of `references` /
`referencesPanel` — appending the index rows behind the server's, in server
order, **deduplicated by (path, line, col)** against the server's list. Each
index row reads its preview line from the synced document (or disk) like a
server location and carries the **`trait` badge** (`ilsp.Reference.Badge`)
the Usages pane and the palette list render as `[trait]` beside the
preview, so the source of a row is visible. The **occurrence highlight**
(`requestDocumentHighlight`) asks the file-local variant when the server
reports no occurrences, so `$this->abc()` inside trait `A` lights up like
any other symbol.

Telemetry records `php.trait.references` with the server's location count
and the rows the index added, only when it added at least one; the
highlight variant records nothing. With `php.trait_index = false`, without a
registered index and in a build without the PHP grammar the merge returns
the server's list untouched. See [lsp](lsp.md#layers) § find references for
the bridge side.

## Wiring

`buildModel` (`internal/app/app.go`) constructs one index per project root
beside `symbols.New(root)` from the flat host config's `[php]` keys and
registers it on the completion engine as an observer; the trait completion
source (#2668) is registered beside it as an ordinary source over the same
index, and gets its telemetry callback once the model's recorder exists. The
navigation fallback's host view (#2670) is registered on the live host in the
same place (`internal/app/phpnav.go` builds it and hangs the telemetry
recorder on it). A project switch rebuilds them with the model.
`Model.PHPIndex()` exposes the index to the features of the later issues.
