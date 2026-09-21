---
type: concept
title: PHP Trait Index
description: The workspace-wide PHP declaration index (Epic 0520, #2667) — every class-like declaration with its members, the trait-use / extends / implements edges between them, and the consumer scope a `$this` inside a trait body resolves against where Intelephense is blind. Built on the shared per-language project walk, kept fresh from buffer edits and watcher events, configured by the [php] section (Settings → PHP).
resource: internal/phpindex
tags: [architecture, php, traits, index, completion, lsp]
timestamp: 2026-09-21T15:00:00Z
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

## Wiring

`buildModel` (`internal/app/app.go`) constructs one index per project root
beside `symbols.New(root)` from the flat host config's `[php]` keys and
registers it on the completion engine as an observer; a project switch
rebuilds it with the model. `Model.PHPIndex()` exposes it to the features
of the later issues.
