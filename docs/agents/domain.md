# Domain Docs

This repository uses a single-context domain layout.

Before exploring a task, read `CONTEXT.md` and `docs/design/LIVE.md` — the current-truth index. Read decision bodies only where `LIVE.md` points; do not glob `docs/design/*.md`, which is roughly 250k tokens, well past the smart zone. Use the vocabulary defined in `CONTEXT.md` in code, tests, issue titles, and handoffs. Surface conflicts with an active design decision instead of silently changing it.

The repository does not use `docs/adr/`; decisions live in `docs/design/` as an append-only log. Every document declares a machine-readable `status` in its frontmatter; anything under `docs/design/superseded/` is void and must never be acted on. Overturning a decision means opening a new record, never rewriting the old one. See `docs/design/README.md`.
