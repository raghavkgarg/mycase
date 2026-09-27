# docs/

Two kinds of documentation live here:

- **[The Mycase Guide](book/README.md)** — in [`book/`](book/). The book: a front-to-back,
  present-tense description of how the system works, organized into parts (Product,
  Architecture, Operations) and governed by [the style guide](book/00-style-guide.md). Start
  at [`book/README.md`](book/README.md) — its preface and contents.

- **Standalone working documents** — at this top level, outside the book. Operational
  trackers and issue logs that record process and history rather than how the system works
  today:
  - [`emb-bug-tracker.md`](emb-bug-tracker.md) — issue tracker for the Early Multibagger
    (`earlymb`) engine.

The line between them: if a document describes the system as it is, it's a book chapter in
`book/`. If it records process, history, or a working log, it's a standalone doc here. When
a working note yields a durable lesson, that reasoning graduates into the relevant chapter;
the note keeps the issue-by-issue detail.
