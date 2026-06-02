# MiniClaw

[![Open in GitHub Codespaces](https://github.com/codespaces/badge.svg)](https://codespaces.new/vii33/MiniClaw)

MiniClaw is a Markdown-configured cyclic task runner for Opencode-powered automations. It runs scheduled tasks from `crons/tasks.yaml`, where each task can run either a shell command or an Opencode instruction.

---

## What It Does

- **Scheduled Tasks**: Reads `crons/tasks.yaml` on each scheduler pass and runs due tasks without a database.
- **Shell Actions**: Executes shell commands for automations such as exports, maintenance, or local scripts.
- **Opencode Instructions**: Sends task instructions through `opencode run` for model-assisted automations.
- **Runtime State**: Tracks last run and last error per task in `.miniclaw/task-state.json`.

---

## File Structure

```
miniclaw/
├── README.md          ← Project overview and scheduler usage
├── .env.example       ← Environment variable template
├── AGENTS.md          ← Core operating rules (read this first)
├── crons/
│   └── tasks.yaml     ← Cron task definitions (schedule, kind, command/instruction)
├── scripts/
│   ├── task-loop.js         ← Polling scheduler that reads tasks.yaml and writes task-state.json
│   └── export-sessions.sh   ← Built-in export helper used by scheduled tasks
├── docs/
│   └── task-scheduler-architecture.md ← Task creation, scheduler behavior, runtime state, and FAQ
└── .miniclaw/
    ├── task-state.json ← Machine-managed runtime state (last_run, last_error per task)
    └── task-loop.lock  ← Continuous scheduler lock with PID and start timestamp
```

All task configuration lives in `crons/tasks.yaml`. Runtime state is tracked in `.miniclaw/task-state.json`: no database, no binary blobs.

---

## Task Definitions

Defined in `crons/tasks.yaml`. Each task has an `id`, `enabled` flag, 5-field cron `schedule`, `kind`, and either a shell `command` or an Opencode `instruction`.

Opencode instruction model:

- Preferred: `zen/minimax2.5-free`
- Automatic fallback when `zen` is not configured: `opencode/minimax-m2.5-free`
- Override manually: `OPENCODE_TASK_MODEL=provider/model`

> **Note:** `scripts/task-loop.js` executes `kind: shell` tasks as allowlisted local script commands. It runs `kind: opencode` tasks with `opencode run` and the selected model.

For the task schema, task-creation checklist, and scheduler FAQ, see [`docs/task-scheduler-architecture.md`](docs/task-scheduler-architecture.md).

---

## Scheduler Operations

Continuous scheduler runs create `.miniclaw/task-loop.lock` before the first poll. The lock stores the scheduler PID and start timestamp, prevents a second live scheduler from starting, and is removed on normal exit. If the recorded PID is no longer live, the next continuous run treats the lock as stale and replaces it. `--once` runs do not acquire the lock.

Run the scheduler loop:

```bash
# Run continuously (poll every 60s)
node scripts/task-loop.js

# Run one iteration
node scripts/task-loop.js --once

# Test due-task matching without side effects
node scripts/task-loop.js --once --dry-run --at 2026-03-07T23:15:00Z
```

Runtime state (last run, last error) is tracked in `.miniclaw/task-state.json`. This file is machine-managed and should not be edited by hand.

Task action execution model:

- `kind: shell` runs the configured `command` after `scripts/task-loop.js` applies task placeholders, verifies the command against the shell allowlist, and rejects unsafe shell syntax.
- `kind: opencode` sends the rendered `instruction` to `opencode run`.
- `OPENCODE_TASK_MODEL` or `--model` selects the model only for `kind: opencode` tasks. Shell tasks do not use this model setting.
- The preferred Opencode task model is `zen/minimax2.5-free`; when that model is unavailable and `opencode/minimax-m2.5-free` is configured, the scheduler falls back automatically.

---

## Further Documentation

- [`docs/task-scheduler-architecture.md`](docs/task-scheduler-architecture.md) for task storage, task creation, scheduler behavior, runtime state, web server-vs-DB trade-offs, Mermaid diagrams, and the scheduler FAQ.
- [`docs/memory-workflow.md`](docs/memory-workflow.md) for the built-in memory workflow.

---

## Prerequisites

- [Opencode](https://opencode.ai/docs/cli/) installed and available to the scheduler.
- For tasks that use Opencode's HTTP API, the Opencode HTTP server must be running:

  ```bash
  opencode serve --port 4096
  ```

---

## Testing in GitHub Codespaces

The fastest way to try MiniClaw is a GitHub Codespace: a cloud VM with everything pre-installed.

1. Click the **Open in Codespaces** badge above (or go to **Code → Codespaces → New codespace**).
2. The container installs `opencode`, `curl`, and `jq` automatically.
3. Inside the terminal:

   ```bash
   # Run one scheduler pass without side effects
   node scripts/task-loop.js --once --dry-run

   # Start the Opencode server for tasks that call its HTTP API
   opencode serve --port 4096
   ```

4. Port `4096` is automatically forwarded, so you can also open `http://localhost:4096/global/health` in the Codespace browser to verify the server is up.

> **Note:** Opencode requires an LLM provider API key. Set it up with `opencode` on first run. It will guide you through provider selection.

---

## Configuration

| Environment Variable | Default | Description |
|---|---|---|
| `OPENCODE_HOST` | `127.0.0.1` | Opencode server hostname |
| `OPENCODE_PORT` | `4096` | Opencode server port |
| `OPENCODE_PASSWORD` | _(none)_ | Optional HTTP basic-auth password |
| `OPENCODE_USERNAME` | `opencode` | HTTP basic-auth username |

---

## Roadmap

- [ ] Add structured scheduler logs and task-level metrics.
- [ ] Add artifact retention and cleanup controls for generated outputs.
- [ ] Scheduler dashboard for task status, logs, and artifacts (stretch goal).
