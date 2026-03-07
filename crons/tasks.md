# Cron Tasks

_Scheduled background tasks for MiniClaw.  
The agent re-reads this file before each run and updates `Last run` after completion._

---

## daily-export
- **Schedule:** `0 23 * * *` _(23:00 every day)_
- **Action:** Run `scripts/export-sessions.sh` to fetch all of today's Opencode sessions via
  the API and write them to `memory/history/YYYY-MM-DD.md`.
- **Last run:** _(never)_

---

## daily-analysis
- **Schedule:** `15 23 * * *` _(23:15 every day, after daily-export)_
- **Action:** Read today's `memory/history/YYYY-MM-DD.md`, summarise what was worked on,
  extract key decisions or learnings, and append them as dated entries to `memory/facts.md`
  under the appropriate headings.
- **Last run:** _(never)_

---

## weekly-review
- **Schedule:** `0 9 * * 1` _(09:00 every Monday)_
- **Action:** Read the past 7 days of `memory/history/` files, produce a short weekly summary,
  and save it to `memory/knowledge/weekly-YYYY-Www.md`.
- **Last run:** _(never)_
