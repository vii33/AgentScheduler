# Implementation Backlog

Implementation backlog for MiniClaw (product/engineering tasks only).

## Core Features

- [ ] Implement session chat loop (CLI).
- [ ] Add weekly-review implementation logic (task is declared in `crons/tasks.yaml`).
- [ ] Add daily-analysis implementation logic (task is declared in `crons/tasks.yaml`).

## Scheduler Hardening

- [ ] Add file locking/single-instance guard for the Go scheduler to prevent duplicate runners.
- [ ] Add tests for cron parsing and due-task detection in the Go scheduler.
- [ ] Add safe handling for large/invalid LLM outputs during opencode task execution.

## Configuration and Extensibility

- [x] Rewrite the task loop in Go for a faster, more reliable long-running scheduler.
- [x] Move task definitions from Markdown (`crons/tasks.md`) to YAML (`crons/tasks.yaml`) and separate mutable runtime state into `.miniclaw/task-state.json`.
- [x] Change model provider from Minimax to a configurable variable (env/config driven, no hardcoded provider).

## Operations

- [ ] Add a background service/runner setup so the Go scheduler runs continuously in background.
- [ ] Add observability: structured scheduler run logs and task-level metrics.

## Product

- [ ] Web UI (stretch goal).

## Natural Next Steps

- [ ] Add a small launcher script/service unit so the Go scheduler runs continuously in background.
- [ ] Add a `--max-tasks` limit or locking mechanism to avoid overlap if multiple loop instances start.
- [ ] Add tests for cron parsing and state file updates in the Go scheduler.