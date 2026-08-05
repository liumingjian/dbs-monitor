# Issue tracker: GitHub

Issues and PRDs for this repo live in GitHub Issues. Use the `gh` CLI for all operations in `liumingjian/dbs-monitor`.

## Conventions

- Map issues are labelled `wayfinder:map` and hold their child tickets.
- Use native GitHub sub-issues for map children. When native sub-issues are unavailable, put `Part of #<map>` at the top of the child body.
- `ready-for-agent` is the label that marks a ticket ready for autonomous implementation.
- PRs are not a request surface for triage.

## Operations

- Read an issue with `gh issue view <number> --comments`.
- Create an issue with `gh issue create`.
- Comment with `gh issue comment <number>`.
- Apply labels with `gh issue edit <number> --add-label`.
- Resolve with a comment followed by `gh issue close`.
