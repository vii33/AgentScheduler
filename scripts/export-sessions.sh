#!/usr/bin/env bash
# export-sessions.sh
#
# Fetches all Opencode sessions from today via the local HTTP API and writes
# them to memory/history/YYYY-MM-DD.md.
#
# Usage:
#   ./scripts/export-sessions.sh [--date YYYY-MM-DD] [--host HOST] [--port PORT]
#
# Dependencies: curl, jq

set -euo pipefail

# ── Configuration ──────────────────────────────────────────────────────────────
HOST="${OPENCODE_HOST:-127.0.0.1}"
PORT="${OPENCODE_PORT:-4096}"
BASE_URL="http://${HOST}:${PORT}"

# Optional HTTP Basic Auth
AUTH_ARGS=()
if [[ -n "${OPENCODE_PASSWORD:-}" ]]; then
  USER="${OPENCODE_USERNAME:-opencode}"
  AUTH_ARGS=(-u "${USER}:${OPENCODE_PASSWORD}")
fi

# ── Argument parsing ────────────────────────────────────────────────────────────
TARGET_DATE="${1:-}"
if [[ "$TARGET_DATE" == "--date" ]]; then
  TARGET_DATE="${2:-}"
  shift 2
fi
TARGET_DATE="${TARGET_DATE:-$(date +%Y-%m-%d)}"

# ── Paths ───────────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
HISTORY_DIR="${REPO_ROOT}/memory/history"
OUT_FILE="${HISTORY_DIR}/${TARGET_DATE}.md"

mkdir -p "$HISTORY_DIR"

# ── Helpers ─────────────────────────────────────────────────────────────────────
api_get() {
  curl -s "${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"}" "${BASE_URL}${1}"
}

check_server() {
  local health
  health=$(api_get "/global/health" 2>/dev/null || true)
  if [[ "$(echo "$health" | jq -r '.healthy // false')" != "true" ]]; then
    echo "ERROR: Opencode server is not reachable at ${BASE_URL}" >&2
    echo "       Start it with:  opencode serve --port ${PORT}" >&2
    exit 1
  fi
}

format_role() {
  case "$1" in
    user)      echo "🧑 user"      ;;
    assistant) echo "🤖 assistant" ;;
    *)         echo "$1"           ;;
  esac
}

# ── Main ────────────────────────────────────────────────────────────────────────
echo "Exporting Opencode sessions for ${TARGET_DATE}..."
check_server

# List all sessions
SESSIONS=$(api_get "/session")
SESSION_COUNT=$(echo "$SESSIONS" | jq 'length')

if [[ "$SESSION_COUNT" -eq 0 ]]; then
  echo "No sessions found."
  exit 0
fi

# Filter sessions created on TARGET_DATE (ISO 8601 prefix match)
TODAY_SESSIONS=$(echo "$SESSIONS" | jq --arg date "$TARGET_DATE" \
  '[.[] | select(.created | startswith($date))]')
TODAY_COUNT=$(echo "$TODAY_SESSIONS" | jq 'length')

if [[ "$TODAY_COUNT" -eq 0 ]]; then
  echo "No sessions found for ${TARGET_DATE}."
  exit 0
fi

echo "Found ${TODAY_COUNT} session(s) for ${TARGET_DATE}."

# ── Write Markdown file ─────────────────────────────────────────────────────────
{
  echo "# Session History — ${TARGET_DATE}"
  echo ""
  echo "_Exported by \`scripts/export-sessions.sh\` on $(date -u +"%Y-%m-%dT%H:%M:%SZ")_"
  echo ""
  echo "---"
  echo ""
  echo "## Sessions"
  echo ""

  echo "$TODAY_SESSIONS" | jq -c '.[]' | while read -r session; do
    SESSION_ID=$(echo "$session" | jq -r '.id')
    TITLE=$(echo "$session"     | jq -r '.title // "Untitled"')
    CREATED=$(echo "$session"   | jq -r '.created // ""')
    MODEL=$(echo "$session"     | jq -r '.model // "unknown"')

    echo "### ${TITLE}"
    echo "- **ID:** \`${SESSION_ID}\`"
    echo "- **Started:** ${CREATED}"
    echo "- **Model:** ${MODEL}"
    echo ""
    echo "#### Conversation"
    echo ""

    MESSAGES=$(api_get "/session/${SESSION_ID}/message")
    MSG_COUNT=$(echo "$MESSAGES" | jq 'length')

    if [[ "$MSG_COUNT" -eq 0 ]]; then
      echo "_No messages._"
    else
      echo "| # | Role | Content |"
      echo "|---|------|---------|"
      echo "$MESSAGES" | jq -c 'to_entries[]' | while read -r entry; do
        IDX=$(echo "$entry"     | jq -r '.key + 1')
        ROLE=$(echo "$entry"    | jq -r '.value.role // "unknown"')
        # Grab first text part; strip newlines and pipe chars for table safety
        CONTENT=$(echo "$entry" | jq -r '
          .value.parts[]? |
          select(.type == "text") |
          .text // ""' \
          | head -c 200 \
          | tr '\n' ' ' \
          | sed 's/|/∣/g')
        echo "| ${IDX} | $(format_role "$ROLE") | ${CONTENT} |"
      done
    fi

    echo ""
    echo "---"
    echo ""
  done

  echo "## Summary"
  echo ""
  echo "_Filled in by the \`daily-analysis\` cron — see \`crons/tasks.md\`._"
  echo ""
} > "$OUT_FILE"

echo "Written to: ${OUT_FILE}"
