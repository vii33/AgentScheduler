#!/usr/bin/env bash
# .devcontainer/post-create.sh
#
# Runs once after the Codespace container is created.
# Installs opencode and prepares the workspace for testing.

set -euo pipefail

echo "── MiniClaw dev setup ──────────────────────────────────────────────────────"

# ── System dependencies ──────────────────────────────────────────────────────
echo "→ Installing system dependencies (curl, jq)..."
sudo apt-get update -qq
sudo apt-get install -y -qq curl jq

# ── Opencode ─────────────────────────────────────────────────────────────────
echo "→ Installing opencode..."
npm install -g opencode-ai

echo "   opencode version: $(opencode --version 2>/dev/null || echo 'installed (version check unavailable)')"

# ── Script permissions ───────────────────────────────────────────────────────
echo "→ Making scripts executable..."
chmod +x scripts/export-sessions.sh

# ── .env setup ───────────────────────────────────────────────────────────────
if [[ ! -f .env ]]; then
  cp .env.example .env
  echo "→ Created .env from .env.example — edit it to add your API keys."
fi

# ── Quick-start hint ─────────────────────────────────────────────────────────
echo ""
echo "── Ready! ──────────────────────────────────────────────────────────────────"
echo ""
echo "  Start Opencode server:   opencode serve --port 4096"
echo "  Export today's sessions: ./scripts/export-sessions.sh"
echo "  Docs:                    cat README.md"
echo ""
