# MiniClaw

A simple, Markdown-first AI assistant that integrates with [Opencode](https://opencode.ai) via its built-in HTTP server. Inspired by Openclaw, but stripped down to the essentials: a persistent memory system and scheduled cron tasks — no database required.

---

## What It Does

- **Chatting** — Sends and receives messages through Opencode's local webserver (`opencode serve`).
- **Memory** — Remembers facts, decisions, and context across sessions using plain Markdown files.
- **Cron Tasks** — Runs scheduled background tasks (e.g. daily summaries, memory consolidation) defined in Markdown.

---

## Architecture

```
miniclaw/
├── AGENTS.md               ← Agent behaviour & instructions (read this first)
├── memory/
│   ├── facts.md            ← Persistent key facts the agent should remember
│   ├── history/            ← Per-session conversation logs (YYYY-MM-DD.md)
│   └── knowledge/          ← Free-form notes and learned context
└── crons/
    └── tasks.md            ← Scheduled task definitions (cron syntax + description)
```

All state lives in Markdown files — no database, no binary blobs.

---

## Prerequisites

- [Opencode](https://opencode.ai/docs/cli/) installed and running:

  ```bash
  opencode serve --port 4096
  ```

---

## Memory System

Memories are stored as Markdown files under `memory/`. There are three layers:

| Layer | File / Folder | Purpose |
|---|---|---|
| **Facts** | `memory/facts.md` | Short, always-relevant facts (name, project, preferences) |
| **History** | `memory/history/YYYY-MM-DD.md` | Full conversation log per session day |
| **Knowledge** | `memory/knowledge/*.md` | Longer notes, summaries, research |

The agent reads `memory/facts.md` on every session start and appends new entries when instructed.

---

## Cron Tasks

Scheduled tasks are declared in `crons/tasks.md` using a simple format:

```markdown
## daily-summary
- **Schedule:** 0 8 * * *
- **Action:** Summarise yesterday's history entry and append key takeaways to memory/facts.md
```

---

## Configuration

| Environment Variable | Default | Description |
|---|---|---|
| `OPENCODE_HOST` | `127.0.0.1` | Opencode server hostname |
| `OPENCODE_PORT` | `4096` | Opencode server port |
| `OPENCODE_PASSWORD` | _(none)_ | Optional HTTP basic-auth password |

---

## Roadmap

- [ ] Implement Opencode HTTP client
- [ ] Memory read/write helpers
- [ ] Cron task runner
- [ ] Session chat loop (CLI)
- [ ] Web UI (stretch goal)
