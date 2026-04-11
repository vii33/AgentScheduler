# Implementation Backlog

Implementation backlog for MiniClaw (product/engineering tasks only).

## Core Features

- [ ] Implement session chat loop (CLI).
- [ ] Add weekly-review implementation logic (currently only declared in `crons/tasks.md`).
- [ ] Add daily-analysis implementation logic (currently only declared in `crons/tasks.md`).

## Scheduler Hardening

- [ ] Add file locking/single-instance guard for `scripts/task-loop.js` to prevent duplicate runners.
- [ ] Add tests for cron parsing and due-task detection.
- [ ] Add tests for `crons/tasks.md` metadata updates (`Last run`, `Last error`).
- [ ] Add safe handling for large/invalid LLM outputs during task compilation.

## Configuration and Extensibility

- [ ] Rewrite the task loop in Go for a faster, more reliable long-running scheduler.
- [x] Change model provider from Minimax to a configurable variable (env/config driven, no hardcoded provider).

## Operations

- [ ] Add a background service/runner setup for continuous scheduling in dev/prod.
- [ ] Add observability: structured scheduler run logs and task-level metrics.

## Product

- [ ] Web UI (stretch goal).

## Natural Next Steps

- [ ] Add a small launcher script/service unit so the loop runs continuously in background.
- [ ] Add a `--max-tasks` limit or locking mechanism to avoid overlap if multiple loop instances start.
- [ ] Add tests for cron parsing and `tasks.md` metadata updates.