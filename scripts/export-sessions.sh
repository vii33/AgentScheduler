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

usage() {
  cat <<'EOF'
Usage:
  ./scripts/export-sessions.sh [--date YYYY-MM-DD] [--host HOST] [--port PORT]

Examples:
  ./scripts/export-sessions.sh
  ./scripts/export-sessions.sh --date 2026-03-07
  ./scripts/export-sessions.sh --host 127.0.0.1 --port 4096
EOF
}

# ── Configuration ──────────────────────────────────────────────────────────────
HOST="${OPENCODE_HOST:-127.0.0.1}"
PORT="${OPENCODE_PORT:-4096}"
TARGET_DATE=""

# ── Argument parsing ────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --date)
      [[ $# -ge 2 ]] || { echo "ERROR: --date requires a value" >&2; usage; exit 1; }
      TARGET_DATE="$2"
      shift 2
      ;;
    --host)
      [[ $# -ge 2 ]] || { echo "ERROR: --host requires a value" >&2; usage; exit 1; }
      HOST="$2"
      shift 2
      ;;
    --port)
      [[ $# -ge 2 ]] || { echo "ERROR: --port requires a value" >&2; usage; exit 1; }
      PORT="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      # Backward-compatible: allow a single positional date argument.
      # Any unknown option-like token should fail fast to avoid silent typos.
      if [[ "$1" == -* ]]; then
        echo "ERROR: Unknown argument: $1" >&2
        usage
        exit 1
      elif [[ -z "$TARGET_DATE" ]]; then
        TARGET_DATE="$1"
        shift
      else
        echo "ERROR: Unknown argument: $1" >&2
        usage
        exit 1
      fi
      ;;
  esac
done

TARGET_DATE="${TARGET_DATE:-$(TZ=Europe/Berlin date +%Y-%m-%d)}"
BASE_URL="http://${HOST}:${PORT}"

# Optional HTTP Basic Auth
AUTH_ARGS=()
if [[ -n "${OPENCODE_PASSWORD:-}" ]]; then
  USER="${OPENCODE_USERNAME:-opencode}"
  AUTH_ARGS=(-u "${USER}:${OPENCODE_PASSWORD}")
fi

# ── Paths ───────────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
HISTORY_DIR="${REPO_ROOT}/memory/history"
OUT_FILE="${HISTORY_DIR}/${TARGET_DATE}.md"
LOG_DIR="${REPO_ROOT}/logs"
LOG_FILE="${LOG_DIR}/export-sessions.log"

mkdir -p "$HISTORY_DIR" "$LOG_DIR"

# ── Helpers ─────────────────────────────────────────────────────────────────────
# Structured log line: timestamp, level, message. Written to logs/export-sessions.log
# so failed/empty runs leave a trace even though no history file is created for them.
log() {
  local level="$1"
  shift
  printf '%s [export-sessions] %s %s\n' "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" "$level" "$*" >> "$LOG_FILE"
}

api_get() {
  curl -s "${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"}" "${BASE_URL}${1}"
}

check_server() {
  local health
  health=$(api_get "/global/health" 2>/dev/null || true)
  if [[ "$(echo "$health" | jq -r '.healthy // false')" != "true" ]]; then
    echo "ERROR: Opencode server is not reachable at ${BASE_URL}" >&2
    echo "       Start it with:  opencode serve --port ${PORT}" >&2
    log "ERROR" "server unreachable at ${BASE_URL} for date=${TARGET_DATE}; no history file written"
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

# List all sessions across projects/workspaces.
SESSIONS=$(api_get "/experimental/session")
SESSION_COUNT=$(echo "$SESSIONS" | jq 'length')

if [[ "$SESSION_COUNT" -eq 0 ]]; then
  echo "No sessions found."
  log "INFO" "no sessions found on server at all; no history file written for date=${TARGET_DATE}"
  exit 0
fi

# Filter sessions created on TARGET_DATE in Europe/Berlin time.
# Supports both legacy shape (.created as ISO 8601 UTC string) and
# experimental shape (.time.created as epoch milliseconds).
TODAY_SESSIONS=$(echo "$SESSIONS" | TZ=Europe/Berlin jq --arg date "$TARGET_DATE" '
  def iso_timestamp:
    capture("^(?<datetime>[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2})(?:\\.(?<fraction>[0-9]+))?(?<offset>Z|[+-][0-9]{2}:?[0-9]{2})$") as $parts |
    ($parts.datetime + "Z" | fromdateiso8601) +
    (if $parts.fraction == null then 0 else ("0." + $parts.fraction | tonumber) end) -
    (if $parts.offset == "Z" then 0
     else ($parts.offset | capture("^(?<sign>[+-])(?<hours>[0-9]{2}):?(?<minutes>[0-9]{2})$") | ((.hours | tonumber) * 3600 + (.minutes | tonumber)) * if .sign == "+" then 1 else -1 end)
     end);
  def session_date:
    if (.created? | type) == "string" then
      (try (.created | iso_timestamp | strflocaltime("%Y-%m-%d")) catch "")
    elif (.time.created? | type) == "number" then
      (.time.created / 1000 | strflocaltime("%Y-%m-%d"))
    else
      ""
    end;
  [.[] | select(session_date == $date)]
')
TODAY_COUNT=$(echo "$TODAY_SESSIONS" | jq 'length')

if [[ "$TODAY_COUNT" -eq 0 ]]; then
  echo "No sessions found for ${TARGET_DATE}."
  log "INFO" "0 sessions matched date=${TARGET_DATE} (of ${SESSION_COUNT} total); no history file written"
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
    CREATED=$(echo "$session"   | TZ=Europe/Berlin jq -r '
      .created //
      (if (.time.created? | type) == "number" then
         (.time.created / 1000 | strflocaltime("%Y-%m-%dT%H:%M:%S%z"))
       else
         ""
       end)')
    MODEL=$(echo "$session"     | jq -r '.model // .version // "unknown"')

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
        ROLE=$(echo "$entry"    | jq -r '.value.role // .value.info.role // "unknown"')
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
  echo "_Filled in by the \`daily-analysis\` cron — see \`crons/tasks.yaml\`._"
  echo ""
} > "$OUT_FILE"

echo "Written to: ${OUT_FILE}"
log "INFO" "wrote ${TODAY_COUNT} session(s) to ${OUT_FILE}"
