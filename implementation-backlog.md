# Implementation Backlog

Implementation backlog for MiniClaw (product/engineering tasks only).

## Scheduler Reliability

- [ ] Add file locking/single-instance guard for `scripts/task-loop.js` to prevent duplicate runners.
- [ ] Add a `--max-tasks` limit or locking mechanism to avoid overlap if multiple loop instances start.
- [ ] Add safe handling for large/invalid opencode task outputs.
- [ ] Validate expected output changes after each scheduled task runs, including file updates and task-state updates.

## Task Schema and Configuration

- [ ] Add stricter validation for `crons/tasks.yaml` task definitions before scheduling.
- [ ] Rewrite the task loop in Go for a faster, more reliable long-running scheduler.
- [x] Change model provider from Minimax to a configurable variable (env/config driven, no hardcoded provider).

## Observability and Operations

- [ ] Add observability: structured scheduler run logs and task-level metrics.
- [ ] Add a background service/runner setup for continuous scheduling in dev/prod.
- [ ] Add a small launcher script/service unit so the loop runs continuously in background.

## Testing

- [ ] Add tests for cron parsing and due-task detection.
- [ ] Add tests for `crons/tasks.yaml` parsing and `.miniclaw/task-state.json` updates.
- [ ] Add integration tests for scheduler runs, expected output validation, and failure-state recording.

## Built-in Workflows

- [ ] Test and refine the weekly-review prompt against representative session-history inputs.
- [ ] Keep daily-analysis workflow behavior aligned with `AGENTS.md`: update `MEMORY.md` and write the history summary.

## Product

- [ ] Implement session chat loop (CLI).
- [ ] Web UI (stretch goal).
