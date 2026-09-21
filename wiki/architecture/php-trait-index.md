---
type: concept
title: PHP Trait Index
description: The workspace-wide PHP declaration index (Epic 0520, #2667) — every class-like declaration with its members, the trait-use / extends / implements edges between them, and the consumer scope a `$this` inside a trait body resolves against where Intelephense is blind. Built on the shared per-language project walk, kept fresh from buffer edits and watcher events, configured by the [php] section (Settings → PHP), with a status popup, a rebuild command and a per-scan telemetry op as its operations surface.
resource: internal/phpindex
tags: [architecture, php, traits, index, completion, navigation, hover, lsp, telemetry, status-line]
timestamp: 2026-09-21T21:00:00Z
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

## Operations (#2673)

The index works in the background and every feature above only shows its
*answers*, so three surfaces say what it is doing.

**Commands.** Both are registered by the app plugin
(`internal/app/commands.go`) and are **global**, not PHP-scoped: the reason to
rebuild — a branch switch with thousands of files, a generator run — is felt
with the explorer or a terminal focused as often as with a `.php` buffer open.

| command | chord | what it does |
| --- | --- | --- |
| `php.traitIndex.status` | `cmd+alt+shift+i` | the status popup below |
| `php.traitIndex.rebuild` | `cmd+alt+shift+b` | drops the project walk and rescans |

A rebuild drops the walk, **not** the observed open buffers: they are the
editor's live truth, and re-reading them from disk would lose the unsaved
overrides. With `php.trait_index = false` or in a build without the PHP
grammar it scans nothing and says so in a notification
(`Index.Rebuild` returns false) rather than pretending a scan started.

**The status popup** (`internal/app/phpindexstatus.go`) is an ordinary
floating-shell modal — esc dismisses it — over `Index.Stats()`:

```
  state          ready
  files          1841
  declarations   2977
  edges          4102
  last scan      612ms
  truncated      yes — the walk stopped at php.index.max_files

  …/projects/legacy-shop
```

`truncated` appears only when the walk hit `php.index.max_files`, and the
root is elided from the left so a deeply nested project cannot widen the box
past the terminal. The two states in which the index can never answer replace
the numbers with the reason: `disabled — php.trait_index is off` and
`unavailable — this build cannot parse PHP (no cgo / no grammar)`. The body
is a snapshot, not a live closure: the popup answers "where is the index
right now", and numbers moving under the reader are harder to report.

**The status line.** While a walk runs — the initial scan or a rebuild — the
`lsp` slot carries `php-index …` beside the focused language's server state
(`php: ready · php-index …`), and nothing once it finished; the slot follows
the buffer, so it only shows for PHP files. The scan's completion already
wakes the Update loop through the content-change callback, so the slot clears
on its own without a timer.

**Telemetry.** One `php.trait.index_scan` op per completed walk — the initial
scan and every rebuild — with `ms`, `files` and `truncated`. The index fires
it through `SetOnScan`, which shares the settle poll with the content-change
callback, so completion is noticed without a second timer. The op is on the
short list that never opens a session file (`startsSession`,
`internal/telemetry`): a walk finishes on its own after every launch into a
PHP project, so opening the log for it would resurrect the ghost file #2318
removed. Together with the
per-feature ops the epic's review recipe reads (references #2671 and rename
#2672 add theirs when they land):

| op | issue | recorded when |
| --- | --- | --- |
| `php.trait.complete` | #2668 | a non-empty trait-member completion answer |
| `php.trait.diag_suppressed` | #2669 | a publish whose undefined-member entries the index resolved |
| `php.trait.definition` | #2670 | a go-to-definition / peek the index answered after an empty server reply |
| `php.trait.hover` | #2670 | a hover card the index filled after an empty server hover |
| `php.trait.index_scan` | #2673 | a completed project walk |

All of them are single `ok` phases recorded only for a non-empty result, so a
reader pairing `start` with `ok` must skip them, and none carries a path,
member name or type name. See
[Usage Telemetry](usage-telemetry.md#event-schema-the-analysis-interface)
§ op ids.

## Wiring

`buildModel` (`internal/app/app.go`) constructs one index per project root
beside `symbols.New(root)` from the flat host config's `[php]` keys and
registers it on the completion engine as an observer; the trait completion
source (#2668) is registered beside it as an ordinary source over the same
index, and gets its telemetry callback once the model's recorder exists. The
navigation fallback's host view (#2670) is registered on the live host in the
same place (`internal/app/phpnav.go` builds it and hangs the telemetry
recorder on it). A project switch rebuilds them with the model.
`Model.PHPIndex()` exposes the index to the features of the later issues, and
`phpIdx.SetOnScan(traitIndexScanRecorder(m.usage))` hangs the per-scan
telemetry op off the same walk (#2673).
