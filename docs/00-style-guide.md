# The Style Guide

This is the book about itself — the one chapter that describes how every *other* chapter
is written. It is the source of truth for the guide's voice, structure, and aesthetic, and
the rubric a chapter is reviewed against before it is added or edited. If a chapter and
this guide disagree, the chapter is wrong.

It exists because the guide is a living book maintained by many hands (and agents) over a
long-lived, growing product. Without a written aesthetic, docs decay back into a pile of
mismatched change-logs: some titled by phase, some opening with "This document
describes…", some carrying stale `Status:` headers and dead absolute-path links. The rules
below are what keep the book coherent — and, like the layering rules in
`.kiro/steering/architecture.md`, they are meant to be *enforced*, not admired.

---

## 1. The governing idea

**A chapter describes the system as it is, in the present tense, for a reader who wants to
understand how it works today.** Everything else follows from that sentence.

- Not *how it came to be* — that is git history.
- Not *what is planned* — that is the Roadmap (Appendix A).
- Not *what broke once* — that is a commit message.

A reader should finish a chapter understanding a subsystem, never having to reconstruct its
project timeline to do so.

---

## 2. Titles

- **One `# H1` per file, and it is a noun phrase naming the subsystem or concept** — the way
  it appears in the book's table of contents. `Storage & Pipeline Persistence`, `Logging &
  Observability`, `Testing`, `Value Strategy`.
- **No `Mycase —` prefix.** The book is about Mycase; every chapter would carry it, so none
  should. (The top-level `README.md` and product-vision framing may name the product; body
  chapters do not.)
- **No process, phase, or version in a title.** Never `Phase 10c — …`, never `… (v3.4)`,
  never `… Refactor`. A title names *what the thing is*, not *when or how it was built*. If
  you are tempted to version a title, the version belongs in the prose (or nowhere).
- **No marketing gloss or parenthetical taglines.** `Value Strategy`, not `Large-Cap Value
  Investment Strategy ("Finding Cheaper Stocks in Big Companies")`.
- **Terse is fine — the module supplies context.** The book's table of contents groups
  chapters into modules, so a one-word title (`Themes`, `Rendering`, `Value`) reads
  correctly in place. Don't pad a title to be self-describing in isolation.

---

## 3. The opening

The first paragraph is the highest-value real estate in the chapter. Spend it on the
*system*, not on the document.

- **Open with a present-tense sentence about what the subsystem is and does.** The reader
  should be learning the system by the end of the first line.
  - ✅ "The pipeline keeps its intermediate state — runs, per-index picks, proposals, and
    final selections — in DuckDB, not in loose CSV files."
  - ✅ "Two pieces of cross-cutting machinery make a mostly-headless quarterly system
    debuggable: structured logging and the raw-response archive."
- **Never open with a meta-sentence about the file.** Banned ledes: "This document
  describes / details / outlines / provides…", "The purpose of this document is…", "This
  guide covers…". They waste the opening and add nothing a reader can't see.
- **Scope or cross-links, if genuinely needed, go in one short line after the opening
  paragraph** — as a `>` note or a single italic sentence, not a metadata block. Prefer a
  woven-in sentence ("The enforced conventions live in `.kiro/steering/logging.md` — this
  chapter is the design behind them.") over a labeled header.

---

## 4. No metadata blocks, no status furniture

Chapters do **not** carry stacked bold-label headers:

- ❌ `**Status**: ✅ IMPLEMENTED` — status lives in the Roadmap; if a chapter exists, the
  thing exists in the present tense.
- ❌ `**Updated**: 2026-09-18` — dates go stale silently and lie; git knows when a file
  changed.
- ❌ `**Purpose**: …` / `**Scope**: …` / `**References**: …` as a header stack — fold the
  one or two that matter into the opening prose, drop the rest.

The only structured furniture a chapter may open with is a single blockquote note when a
chapter needs a genuine caveat (e.g. "> Part of the India-Path." on a market-path-specific
chapter). One line, not a table.

---

## 5. Present tense; history and status live elsewhere

- **Describe behavior, not change.** "The Router caches merged fundamentals" — not "Phase
  10e made the Router cache fundamentals."
- **No `~~struck-through~~ ✅ FIXED` archaeology, no "was broken / now works," no incident
  narratives** ("## The Incident & Error Analysis"). If a subsystem exists because an
  earlier design failed, state the *current* rationale ("fundamentals are cached in the
  Router, not the client, because the client blob is pre-overlay") — the reader needs the
  reason, not the postmortem.
- **No resolved-bug ledgers as chapters.** A tracker of `Bug-001 … Resolved` entries is
  process, not documentation — it belongs in git history and, if a fix changed a durable
  behavior, that behavior is described in the relevant chapter.
- **One chapter per subsystem or concept — never per phase, refactor, or incident.** A doc
  titled by a *process* is a smell; retitle it by the subsystem, or fold it into the
  Roadmap if it is purely status.

---

## 6. Links and code references

- **Never hard-code an absolute filesystem path.** Links like
  `file:///Users/<somebody>/Projects/.../pkg/foo.go` are machine-specific, break on every
  other checkout, and rot silently. They are banned.
- **Reference code by repo-relative path in backticks**: `` `pkg/datafetcher/merger.go` ``,
  `` `cmd/pick.go` ``. Add a line range in prose only when it genuinely aids navigation, and
  accept that ranges drift — prefer naming the function/type (`` `Router.Fundamentals` ``)
  over a line number.
- **Cross-link other chapters by relative filename**: `[Storage](10-storage.md)`,
  `docs/04-architecture.md`. When a chapter is renamed, every such link is migrated in the
  same change (grep `docs/NN-*.md`), and no link may point at a file that doesn't exist.
- **Refer to a chapter by its title or module position, not a stale number**, in running
  prose where practical — filenames carry the number, the prose carries the meaning.

---

## 7. Structure and rhythm

- **Prose first; tables and lists for what is genuinely enumerable.** A table earns its
  place for a set of parallel facts (channels, commands, coverage tiers, config keys). Don't
  render an argument as a bullet list that wants to be a paragraph.
- **Section numbering is optional and used only when a chapter is long enough that readers
  cross-reference sections.** Short chapters (`Testing`, `Rendering`) need no `## 1.` /
  `## 2.` scaffolding. Be consistent within a chapter: number all top-level sections or none.
- **A table of contents is for long reference chapters only** (Architecture, Data Sources,
  Runbook). A five-screen chapter doesn't need one.
- **One `---` rule after the opening**, and thereafter only to separate major sections in a
  long chapter. Don't pepper the file with horizontal rules.
- **Code blocks are illustrative, not exhaustive.** Show the shape of a struct, a config
  block, a representative command — not a wholesale paste of a source file.

---

## 8. Voice

- Direct, technical, and specific. Name the package, the type, the command. Precision is the
  aesthetic — a reader should trust that a named symbol exists and does what the sentence
  says.
- Explain the *why* behind a non-obvious choice, in one or two sentences, in the present
  tense ("single-binary local tool, so minimal dependencies is a constraint — hence stdlib
  `slog` over zap"). Rationale is welcome; history is not.
- Assume a competent reader. Don't re-teach Go, DuckDB, or finance basics; do define
  project-specific terms (a "golden copy," the "India-Path") on first use.
- Em dashes and terse asides are part of the house voice — used for a beat of emphasis or a
  parenthetical clarification, not as a crutch on every line.

---

## 9. The book's shape

The chapters read front-to-back as a book, grouped into modules (see `README.md`, which is
the module-grouped table of contents). Two structural rules:

- **Filenames are stable numeric IDs (`NN-slug.md`); the slug matches the chapter title.**
  Renaming a chapter is a deliberate, wholesale change (rename + migrate every reference +
  build), never piecemeal.
- **Status and plans are not part of the reading spine.** The Roadmap is the one
  consult-don't-read document; it lives as **Appendix A**, not as a numbered chapter in the
  middle of the narrative. Anything that reads as "what's done / in progress / next" belongs
  there, not in a chapter.

---

## 10. The checklist

Before adding or editing a chapter, confirm:

1. Title is a bare noun phrase — no `Mycase —`, no phase, no version, no tagline.
2. First sentence describes the system in the present tense — no "This document…".
3. No `Status:` / `Updated:` / `Purpose:`-stack metadata headers.
4. No history, incident narrative, resolved-bug ledger, or `✅ FIXED` archaeology.
5. Every code reference is a repo-relative backtick path — zero `file:///…` absolute links.
6. Every chapter cross-link resolves to a file that exists.
7. Prose carries the argument; tables/lists only carry enumerable facts.
8. `---` rules and section numbering are minimal and consistent within the chapter.
9. The chapter documents exactly one subsystem or concept.
10. It is listed in the `README.md` table of contents under the right module.
