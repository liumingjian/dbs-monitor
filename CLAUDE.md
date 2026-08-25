# dbs-monitor

Self-hosted PostgreSQL monitoring platform. Go backend + agent, TS/React SPA (embedded into the main binary via `go:embed`).
This file has a 150-line budget; it only records rules whose violation `make check` will not catch.

## Agent skills

### Issue tracker

GitHub Issues is this repository's issue tracker. The `ready-for-agent` label means the issue is ready to hand off for unattended implementation.

### Domain docs

This repository is single-context. Read `CONTEXT.md` (terminology) before starting work.
