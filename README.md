# MiniClaw

[![Open in GitHub Codespaces](https://github.com/codespaces/badge.svg)](https://codespaces.new/vii33/MiniClaw)

MiniClaw is a Markdown-configured Go task runner for Opencode-powered automations. It runs scheduled tasks from `crons/tasks.yaml`, where each task can run either a shell command or an Opencode instruction.

---

## What It Does

- **Scheduled Tasks**: Reads `crons/tasks.yaml` on each scheduler pass and runs due task slots.
- **Missed-Run Handling**: Uses `skip`, `run-latest`, or `catch-up` policies when the PC was asleep or offline.
- **Shell Actions**: Executes shell commands for automations such as exports, maintenance, or local scripts.
- **Opencode Instructions**: Sends task instructions through `opencode run` for model-assisted automations.
- **Runtime History**: Tracks task attempts, outcomes, durations, and missed schedule slots in `miniclaw.db`.

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
│   └── export-sessions.sh   ← Built-in export helper used by scheduled tasks
├── cmd/
│   └── task-loop/           ← Go scheduler that reads tasks.yaml and writes miniclaw.db
├── docs/
│   └── task-scheduler-architecture.md ← Task creation, scheduler behavior, runtime state, and FAQ
├── miniclaw.db        ← Machine-managed SQLite task run history (generated)
└── task-loop.lock     ← Continuous scheduler lock with PID and start timestamp (generated)
```

All task configuration lives in `crons/tasks.yaml`. Runtime history is tracked in `miniclaw.db` with SQLite so task status queries stay simple and unambiguous.

---

## Task Definitions

Defined in `crons/tasks.yaml`. Each task has an `id`, `enabled` flag, 5-field cron `schedule`, optional missed-run policy, `kind`, and either a shell `command` or an Opencode `instruction`.

Opencode instruction model:

- Preferred: `zen/minimax2.5-free`
- Automatic fallback when `zen` is not configured: `opencode/minimax-m2.5-free`
- Override manually: `OPENCODE_TASK_MODEL=provider/model`

> **Note:** `cmd/task-loop` executes `kind: shell` tasks as allowlisted local script commands. It runs `kind: opencode` tasks with `opencode run` and the selected model.

For the task schema, task-creation checklist, and scheduler FAQ, see [`docs/task-scheduler-architecture.md`](docs/task-scheduler-architecture.md).

---

## Scheduler Operations

Continuous scheduler runs create `task-loop.lock` before the first poll. The lock stores the scheduler PID and start timestamp, prevents a second live scheduler from starting, and is removed on normal exit. If the recorded PID is no longer live, the next continuous run treats the lock as stale and replaces it. `--once` runs do not acquire the lock.

Run the scheduler loop:

```bash
# Run continuously (poll every five minutes by default)
go run ./cmd/task-loop

# Run one iteration
go run ./cmd/task-loop --once

# Test due-slot matching without side effects
go run ./cmd/task-loop --once --dry-run --at 2026-03-07T23:15:00Z
```

Runtime history is tracked in `miniclaw.db`. This SQLite database is machine-managed and should not be edited by hand outside intentional maintenance.

Task action execution model:

- `kind: shell` runs the configured `command` after `cmd/task-loop` applies task placeholders, verifies the command against the shell allowlist, and rejects unsafe shell syntax.
- `kind: opencode` sends the rendered `instruction` to `opencode run`.
- `OPENCODE_TASK_MODEL` or `--model` selects the model only for `kind: opencode` tasks. Shell tasks do not use this model setting.
- The preferred Opencode task model is `zen/minimax2.5-free`; when that model is unavailable and `opencode/minimax-m2.5-free` is configured, the scheduler falls back automatically.

---

## Further Documentation

- [`docs/task-scheduler-architecture.md`](docs/task-scheduler-architecture.md) for task storage, task creation, scheduler behavior, SQLite runtime history, missed-run handling, Mermaid diagrams, and the scheduler FAQ.
- [`docs/memory-workflow.md`](docs/memory-workflow.md) for the built-in memory workflow.

---

## Prerequisites

- Go installed for `go run ./cmd/task-loop`.
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
   go run ./cmd/task-loop --once --dry-run

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
| `OPENCODE_TASK_MODEL` | `zen/minimax2.5-free` | Default model for `kind: opencode` tasks |
| `TASK_LOOP_POLL_SECONDS` | `300` | Scheduler polling interval in seconds |

---

## Local Testing

```bash
# Unit and integration-style scheduler tests
go test ./...

# Static checks
go vet ./...
```

The Go test suite covers cron slot matching, missed-run policy selection, SQLite run recording, duplicate-slot prevention, failure recording, catch-up behavior, and dry-run no-side-effect behavior.

---

## Roadmap

- [ ] Add a friendly task status command on top of `miniclaw.db`.
- [ ] Add bounded catch-up limits for tasks with `missed: catch-up`.
- [ ] Add structured scheduler logs and task-level metrics.
- [ ] Add artifact retention and cleanup controls for generated outputs.
- [ ] Scheduler dashboard for task status, logs, and artifacts (stretch goal).
