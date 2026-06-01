# Implementation Backlog

Implementation backlog for MiniClaw's scheduler, task definitions, runtime operations, observability, and generated artifacts.

## Scheduler Operations

- [ ] Add file locking/single-instance guard for `scripts/task-loop.js` to prevent duplicate runners.
- [ ] Add a background service/runner setup for continuous scheduling in dev/prod.
- [ ] Add a small launcher script/service unit so the loop runs continuously in background.
- [ ] Add a `--max-tasks` limit or locking mechanism to avoid overlap if multiple loop instances start.
- [ ] Rewrite the task loop in Go for a faster, more reliable long-running scheduler.

## Task Definitions and Execution

- [ ] Add weekly-review implementation logic (declared in `crons/tasks.yaml`).
- [ ] Add daily-analysis implementation logic (declared in `crons/tasks.yaml`).
- [x] Change model provider from Minimax to a configurable variable (env/config driven, no hardcoded provider).
- [ ] Add safe handling for large/invalid Opencode task outputs.

## Observability and Runtime State

- [ ] Add structured scheduler run logs and task-level metrics.
- [ ] Add tests for cron parsing and due-task detection.
- [ ] Add tests for `crons/tasks.yaml` parsing and `.miniclaw/task-state.json` updates.

## Artifact Management

- [ ] Define retention rules for generated history and knowledge artifacts.
- [ ] Add artifact cleanup or archiving for old scheduler outputs.
- [ ] Add artifact manifest metadata so generated files can be traced back to task runs.

## Scheduler Dashboard

- [ ] Scheduler dashboard for task status, logs, and artifacts (stretch goal).
