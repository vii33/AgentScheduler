# MEMORY.md — Synthesised Preferences

_This file is distilled from daily session exports in `memory/history/`.
The `daily-analysis` cron updates it. Keep entries concise.
Read this at the start of every session._

---

## User Preferences
<!-- Coding style, tool choices, workflow preferences -->
<!-- - YYYY-MM-DD: <fact> -->
- 2026-06-03: User prefers candid, opinionated feedback and is okay with calling out bad ideas directly.
- 2026-06-12: User reaffirmed they prefer strong, candid opinions and direct pushback on bad ideas.

## Project Context
<!-- Active projects, goals, current focus areas -->
<!-- - YYYY-MM-DD: <fact> -->
- 2026-03-07: Daily session exports are written to `memory/history/YYYY-MM-DD.md` via `scripts/export-sessions.sh`.

## Decisions
<!-- Architectural choices, agreed approaches, tool selections -->
<!-- - YYYY-MM-DD: <fact> -->

## Lessons Learned
<!-- Bugs fixed, patterns discovered, things not to repeat -->
<!-- - YYYY-MM-DD: <fact> -->
- 2026-07-27: The `daily-analysis` cron fabricated a "JSON-only" / `bats-core` user preference from a one-off test session on 2026-03-07 and then re-confirmed it daily with no real new sessions behind it, duplicating it ~15x across `MEMORY.md`. The user never stated this preference. Root cause fixed: `export-sessions.sh` now logs (instead of silently no-op'ing) when no history file is written, and the `daily-analysis` instruction now skips the memory update entirely (and logs the skip) when today's history file doesn't exist.

## To Follow Up
<!-- Open questions, reminders, loose ends -->
<!-- - YYYY-MM-DD: <fact> -->

---

_Specific session logs live in `memory/history/`. This file stays concise._
