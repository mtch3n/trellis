#!/usr/bin/env bash
set -euo pipefail

# Browser-level P3/P5 smoke coverage. The test deliberately uses a fresh
# project and database so it cannot pass because of a developer's local state.

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PORT=${TRELLIS_UI_E2E_PORT:-7798}
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/trellis-ui-e2e.XXXXXX")
TRELLIS_HOME_E2E="$TMP_ROOT/home"
TRELLIS_REPO_E2E="$TMP_ROOT/repo"
TRELLIS_BIN_E2E="$TMP_ROOT/trellis"
SERVER_LOG="$TMP_ROOT/server.log"
SESSION=""
SERVER_PID=""

cleanup() {
  if [[ -n "$SESSION" ]]; then
    npx --yes agent-browser --session "$SESSION" close >/dev/null 2>&1 || true
  fi
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP_ROOT"
}
trap cleanup EXIT

mkdir -p "$TRELLIS_HOME_E2E" "$TRELLIS_REPO_E2E"
git -C "$TRELLIS_REPO_E2E" init -q
PNPM_CONFIG_MINIMUM_RELEASE_AGE=0 pnpm --dir "$ROOT/web" run build >/dev/null
GOCACHE="${GOCACHE:-/tmp/trellis-gocache}" go build -o "$TRELLIS_BIN_E2E" "$ROOT/cmd/trellis"

run_trellis() {
  (cd "$TRELLIS_REPO_E2E" && TRELLIS_HOME="$TRELLIS_HOME_E2E" "$TRELLIS_BIN_E2E" "$@")
}

run_trellis init >/dev/null
run_trellis card new --title "Seed card" --body "seed body" >/dev/null
run_trellis card new --title "Second seed card" --body "second seed body" >/dev/null
run_trellis card claim REPO-1 --as "seed-agent" >/dev/null
run_trellis knowledge new --title "Browser note" --summary "browser coverage" --body $'# Seed\n\n- first\n- second\n\n```\nready\n```' >/dev/null

TRELLIS_HOME="$TRELLIS_HOME_E2E" "$TRELLIS_BIN_E2E" ui --port "$PORT" >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$PORT/" >/dev/null 2>&1; then break; fi
  sleep 0.1
done
curl -fsS "http://127.0.0.1:$PORT/" >/dev/null

SESSION=$(npx --yes agent-browser session id --scope worktree --prefix trellis-ui-e2e)
ab() { npx --yes agent-browser --session "$SESSION" "$@"; }

ab open "http://127.0.0.1:$PORT/"
ab wait --text "Projects"
ab find role link click --name "Open REPO"
ab wait --url "**/p/REPO/b/**"
ab find testid "board-column-backlog" text

# Create, edit, and move a card through the board UI.
ab find placeholder "Card title" fill "Browser-created card"
ab find placeholder "Context or acceptance notes" fill "Created through browser coverage"
ab find role button click --name "Create card"
ab wait --text "Browser-created card"
ab find testid "board-card-REPO-3" click
ab find label "Card title editor" fill "Browser-edited card"
ab find label "Card body editor" fill "Edited Markdown body"
ab find role button click --name "Save changes"
ab wait --text "Browser-edited card"
ab drag '[data-testid="board-card-REPO-3"]' '[data-testid="board-column-in-progress"]'
ab wait --fn "document.querySelector('[data-testid=\"board-column-in-progress\"] [data-testid=\"board-card-REPO-3\"]') !== null"

# Same-column reorder and SSE refresh from an independent CLI writer.
ab drag '[data-testid="board-card-REPO-3"]' '[data-testid="board-card-REPO-1"]'
ab wait --fn "document.querySelector('[data-testid=\"board-column-backlog\"] [data-testid=\"board-card-REPO-3\"]') !== null"
run_trellis card new --title "SSE-created card" --body "created outside the browser" >/dev/null
ab wait --text "SSE-created card"

# Knowledge editor, Typeset Markdown preview, graph, and label merge.
ab find role link click --name "Knowledge"
ab wait --text "Knowledge base"
ab find text "Browser note" click --exact
ab wait --text "Seed"
ab find role heading text --name "Seed"
ab wait --text "Linked graph"
ab find placeholder "from" fill "bug"
ab find placeholder "into" fill "question"
ab find role button click --name "Merge"
ab wait --text "Available:"

# The seeded lease makes the steal-from-UI path visible and executable.
ab find role link click --name "Board"
ab wait --text "Seed card"
ab find testid "board-card-REPO-1" click
ab find label "Steal reason" fill "browser lease takeover"
ab find role button click --name "Steal lease"
ab wait --text "browser lease takeover"

echo "UI browser smoke passed (P3 board flows and P5 knowledge/graph/labels/lease flows)."
