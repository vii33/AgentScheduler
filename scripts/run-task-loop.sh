#!/usr/bin/env bash
# Run the AgentScheduler continuous task loop from the repository root.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$REPO_ROOT"
exec go run ./cmd/task-loop
