#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || ! $1 =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]]; then
  echo "usage: $0 YYYY-MM-DD" >&2
  exit 2
fi

exec go run ../teams-daily-bot/cmd/daily-attendees reconcile \
  --date "$1" \
  --config ../teams-daily-bot/config/live.json \
  --apply --yes --no-input --json
