# dbs-monitor

`dbs-monitor` is a database monitoring system project.

## Purpose

This repository is used to develop tools for observing database health, performance, availability, and operational signals.

## Documentation

Start from [`docs/README.md`](docs/README.md).

PostgreSQL MVP product/design specification (R1, v1.0):

- [`docs/design/00-decision-index.md`](docs/design/00-decision-index.md) — R1 decision index: conclusions, rationale, and **rejected alternatives**. Read this first.
- [`docs/design/01-pg-mvp-metric-dictionary.md`](docs/design/01-pg-mvp-metric-dictionary.md) — PG MVP metric dictionary.
- [`docs/design/02-alert-rule-model-draft.md`](docs/design/02-alert-rule-model-draft.md) — Alert rule model.
- [`docs/design/03-monitor-platform-ia-draft.md`](docs/design/03-monitor-platform-ia-draft.md) — Monitoring platform information architecture.
- [`docs/research/aliyun-rds/aliyun-rds-pg-monitor-feasibility-report.md`](docs/research/aliyun-rds/aliyun-rds-pg-monitor-feasibility-report.md) — Aliyun RDS PostgreSQL monitoring feasibility research.

Research screenshots and snapshots are under [`docs/research/aliyun-rds/evidence/`](docs/research/aliyun-rds/evidence/).

## Planned Scope

- Database connection and availability checks
- Metrics collection for common database engines
- Alerting and notification workflows
- Dashboard and reporting interfaces
- Deployment and operations documentation

## Status

This project is in the initial research and design stage.
