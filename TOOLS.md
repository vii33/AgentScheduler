# TOOLS.md — Environment Config

Environment-specific values only: paths, ports, and where secrets live.
`AGENTS.md` defines how tools work; this file holds the lookup values.

## Opencode Server

| Setting | Value |
|---------|-------|
| Base URL | `http://127.0.0.1:4096` |
| Health | `GET /global/health` |
| Sessions | `GET /session` |
| Messages | `GET /session/:id/message` |
| Chat | `POST /session/:id/message` |
| Events (SSE) | `GET /global/event` |

Auth: set `OPENCODE_PASSWORD` (and optionally `OPENCODE_USERNAME`) in `.env`.

## Paths

| Resource | Path |
|----------|------|
| Memory root | `./memory/` |
| Daily session exports | `./memory/history/YYYY-MM-DD.md` |
| Knowledge notes | `./memory/knowledge/` |
| Synthesised memory | `./MEMORY.md` |
| Export script | `./scripts/export-sessions.sh` |
| Cron definitions | `./crons/tasks.md` |

## Secrets

Store secrets in `.env` at the repo root (gitignored). See `.env.example` for the canonical list.
