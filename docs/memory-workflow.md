# Memory Workflow

This document covers the Memory System example built on AgentScheduler: scheduled session export, daily analysis, weekly review, memory-file responsibilities, and memory hygiene. The memory workflow is an example scheduler configuration, not the scheduler's whole purpose.

For task creation, scheduler internals, runtime state, and cron architecture, see [`docs/task-scheduler-architecture.md`](task-scheduler-architecture.md).

## Overview

Memory is a scheduled workflow built on the same task runner primitives as any other automation.

Two layers, both plain Markdown:

| Layer | File | When loaded | Purpose |
|---|---|---|---|
| **Synthesised** | `MEMORY.md` | Every session | Distilled preferences, decisions, lessons |
| **Daily raw** | `memory/history/YYYY-MM-DD.md` | On demand / by cron | Full session export for that day |

The `daily-export` shell task writes raw session history. The `daily-analysis` Opencode instruction reads the raw export and promotes important notes into `MEMORY.md`.

## Built-in memory tasks

Defined in `crons/tasks.yaml`:

| Task | Schedule | Action Type | Action |
|---|---|---|---|
| `daily-export` | 23:00 daily | shell | Export sessions to `memory/history/YYYY-MM-DD.md` |
| `daily-analysis` | 23:15 daily | Opencode instruction | Analyse export, update `MEMORY.md`, and write a summary into the daily history file |
| `weekly-review` | 09:00 Monday | Opencode instruction | Summarise the week into `memory/knowledge/weekly-YYYY-Www.md` |

## Manual export

You can run the export manually:

```bash
# Export today's sessions manually
./scripts/export-sessions.sh

# Export a specific date
./scripts/export-sessions.sh --date 2026-03-07
```

The export helper requires `curl` and `jq`. It reads `OPENCODE_HOST`, `OPENCODE_PORT`, `OPENCODE_PASSWORD`, and `OPENCODE_USERNAME` from the environment or `.env`.

## Memory file responsibilities

### Daily memory: `memory/history/YYYY-MM-DD.md`

Purpose:

- raw export of the day's Opencode sessions
- full transcript record
- audit trail for what happened on a specific day

Why it exists:

- raw detail is useful for recovery, debugging, and later analysis
- it should not all be loaded into every future session

### Weekly memory: `memory/knowledge/weekly-YYYY-Www.md`

Purpose:

- higher-level summary across multiple daily files
- a way to retain cross-day patterns without reopening every raw history file

Why it exists:

- daily logs are too detailed for routine reuse
- weekly summaries are a compression layer between raw history and durable memory

### Durable memory: `MEMORY.md`

Purpose:

- small, always-loaded memory for preferences, decisions, lessons, and follow-ups

Why it exists:

- the assistant needs a stable short context window
- long transcripts would pollute context and reduce quality

### Scratch memory: `memory/facts.md`

Purpose:

- lightweight scratch pad for quick reminders that do not belong in durable memory yet

## Where the memory-compression prompts are

There are three places worth knowing about.

### Task instructions in `crons/tasks.yaml`

The current source of truth for what `daily-analysis` and `weekly-review` should do is their `instruction` text in `crons/tasks.yaml`.

That means memory-compression behavior is defined in YAML task config, not in Markdown task prose.

### Execution logic in `cmd/task-loop`

The Go scheduler no longer compiles task prose into another format first.

Instead:

- `kind: shell` tasks run the configured `command` after placeholder rendering, shell allowlist checks, and unsafe shell syntax rejection.
- `kind: opencode`, `copilot-cli`, `claude`, `codex`, and `pi-agent` tasks send the rendered `instruction` to the matching local CLI.
- `OPENCODE_TASK_MODEL` or `--model` selects the default model only for `kind: opencode` tasks; task-level `model` and per-agent environment variables select models for other agent kinds.
- `thinking` is available for `kind: opencode` tasks and maps to Opencode's provider-specific `--variant` flag.

So the generic execution logic lives in code, but the memory-specific shell commands and Opencode prompt text live in `crons/tasks.yaml`.

### Runtime history in `agentscheduler.db`

For memory tasks:

- task definitions and prompts live in `crons/tasks.yaml`
- runtime execution history lives separately in `agentscheduler.db`
- editing the SQLite database changes run metadata, not memory behavior

## How to keep memory from getting polluted over time

The current architecture already has some guardrails.

### Existing guardrails

From `AGENTS.md` and `MEMORY.md`:

- keep `MEMORY.md` concise
- append one-line entries under the correct heading
- never delete old entries silently; strike through outdated ones instead
- move longer notes into `memory/knowledge/<topic>.md`
- keep raw transcripts in `memory/history/`, not in always-loaded memory
- use `memory/facts.md` as a scratch pad for short-lived reminders

### Practical operating rules

If you want clean long-term memory, follow this discipline:

1. Put only durable facts into `MEMORY.md`.
2. Keep session detail in `memory/history/`.
3. Move long-form analysis into `memory/knowledge/`.
4. Strike through stale entries in `MEMORY.md` instead of rewriting history.
5. Review weekly notes before promoting anything into durable memory.
6. Avoid storing temporary task output in always-loaded files.

### Current limitation

This project relies on human and prompt discipline. It does **not** yet enforce:

- a hard size budget for `MEMORY.md`
- automatic pruning or archival
- deduplication of repeated facts
- confidence scoring for promoted memories

## How to create or change a memory task

Memory System tasks are normal AgentScheduler tasks. To add one:

1. Add a new task object to `crons/tasks.yaml`.
2. Use `kind: shell` for local scripts or `kind: opencode` for model-assisted analysis.
3. Give the task an explicit output path, such as `memory/knowledge/my-topic.md`.
4. Keep durable facts concise and write long-form notes under `memory/knowledge/`.
5. Pick a `missed` policy; `run-latest` is the default and is best for most memory tasks.
6. Test schedule matching with `go run ./cmd/task-loop --once --dry-run --at <ISO timestamp>`.

Example:

```yaml
  - id: monthly-memory-review
    enabled: true
    schedule: "0 9 1 * *"
    missed: run-latest
    kind: opencode
    instruction: |
      Review memory/knowledge/ and MEMORY.md.
      Identify outdated or duplicated durable memory entries.
      Write recommendations to memory/knowledge/monthly-review-YYYY-MM-DD.md.
```

For the full task schema and scheduler details, see [`docs/task-scheduler-architecture.md`](task-scheduler-architecture.md#how-to-create-a-new-task).

## TODO: Agent Brain session-review integration

Add a scheduled or external session-review task that loads OpenCode sessions and writes durable learnings to Agent Brain.

Reviewer instructions:

- At the end of reviewing an OpenCode session, decide whether the session produced durable memory.
- If yes, call Agent Brain `memory_create`; optionally call `memory_search` first to avoid duplicates.
- Save only stable project facts, decisions, reusable analysis findings, architecture/convention/gotchas, and user preferences.
- Do not save raw transcripts, temporary chatter, secrets/tokens/personal data, one-off commands unless reusable, or uncertain guesses.
- Use `source: "session-review"`, `scope: "workspace"`, `workspace_id` from the git repo/workspace slug, lowercase OS `user_id`, and the OpenCode `session_id` when available.
- Prefer producing structured memory candidates first, then let the wrapper/tool executor perform `memory_search` and `memory_create`.

## FAQ

### Why do daily and weekly memory both exist?

Daily history keeps raw transcripts for auditability and recovery. Weekly notes keep higher-level summaries without loading every daily file into routine context.

### Where are the prompts for memory compression tasks?

They live in the `instruction` fields of `crons/tasks.yaml` for `kind: opencode` tasks such as `daily-analysis` and `weekly-review`.

### How do I prevent memory pollution?

Keep `MEMORY.md` short, move long notes to `memory/knowledge/`, strike outdated facts instead of silently changing them, and keep raw history in `memory/history/`.

### Should raw transcripts go into `MEMORY.md`?

No. Raw transcripts belong in `memory/history/YYYY-MM-DD.md`. `MEMORY.md` should contain only concise, durable preferences, decisions, lessons, and follow-ups.

### Can memory tasks be disabled?

Yes. Set `enabled: false` for the relevant task in `crons/tasks.yaml`.

### What should I inspect when a memory task fails?

Query `agentscheduler.db` for failed `task_runs`, then inspect the output path named by the task. For export failures, also verify that the Opencode HTTP server is healthy and that `curl` and `jq` are installed.

## File reference

| Purpose | File |
|---|---|
| Memory task definitions and prompts | `crons/tasks.yaml` |
| Session export script | `scripts/export-sessions.sh` |
| Durable synthesised memory | `MEMORY.md` |
| Daily raw history | `memory/history/YYYY-MM-DD.md` |
| Daily history template | `memory/history/TEMPLATE.md` |
| Weekly or topical long-form notes | `memory/knowledge/` |
| Knowledge template | `memory/knowledge/TEMPLATE.md` |
| Runtime task history | `agentscheduler.db` |
| Scheduler architecture | `docs/task-scheduler-architecture.md` |
