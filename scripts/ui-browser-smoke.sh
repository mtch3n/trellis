#!/usr/bin/env bash
set -euo pipefail

# Browser-level smoke coverage for the web UI: the board, the vault, a claim
# stolen from the UI, settings, and search. The test uses a fresh project and
# database of its own, so it cannot pass because of a developer's local state,
# and it never reads or writes the real Trellis home.
#
# What it deliberately does not cover: dragging a card between columns. The
# board's drag is pointer-driven with no stable handle to aim at, so the move
# is exercised through the card's Column control, which is the same write.

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PORT=${TRELLIS_UI_E2E_PORT:-7798}
# A run killed before its EXIT trap leaves its directory behind; an hour-old
# one belongs to no live run.
find "${TMPDIR:-/tmp}" -maxdepth 1 -name 'trellis-ui-e2e.*' -mmin +60 -exec rm -rf {} + 2>/dev/null || true
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/trellis-ui-e2e.XXXXXX")
TRELLIS_HOME_E2E="$TMP_ROOT/home"
TRELLIS_REPO_E2E="$TMP_ROOT/repo"
TRELLIS_BIN_E2E="$TMP_ROOT/trellis"
SERVER_LOG="$TMP_ROOT/server.log"
SESSION=""
SERVER_PID=""

cleanup() {
  if [[ -n "$SESSION" ]]; then
    pnpm dlx agent-browser --session "$SESSION" close >/dev/null 2>&1 || true
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
(cd "$ROOT" && go build -o "$TRELLIS_BIN_E2E" ./cmd/trellis)

# Every call carries the scratch home: a binary built from the branch must
# never open the developer's own database, not even to print help.
run_trellis() {
  (cd "$TRELLIS_REPO_E2E" && TRELLIS_HOME="$TRELLIS_HOME_E2E" "$TRELLIS_BIN_E2E" "$@")
}

run_trellis init >/dev/null
run_trellis card new --title "Seed card" --body "seed body" >/dev/null
run_trellis card new --title "Second seed card" --body "second seed body" >/dev/null
run_trellis card claim REPO-1 --as "seed-agent" >/dev/null
run_trellis vault new --title "Browser note" --summary "browser coverage" \
  --body $'# Seed\n\n- first\n- second\n\n```\nready\n```' >/dev/null

TRELLIS_HOME="$TRELLIS_HOME_E2E" "$TRELLIS_BIN_E2E" ui --port "$PORT" >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$PORT/" >/dev/null 2>&1; then break; fi
  sleep 0.1
done
curl -fsS "http://127.0.0.1:$PORT/" >/dev/null

# The daemon hands out its session cookie only to the authenticated URL it
# printed, so the browser has to open that one. The line lands in the log a
# moment after the port opens.
TOKEN=""
for _ in $(seq 1 50); do
  TOKEN=$(grep -o 'token=[A-Za-z0-9]*' "$SERVER_LOG" | head -1 | cut -d= -f2 || true)
  [[ -n "$TOKEN" ]] && break
  sleep 0.1
done
if [[ -z "$TOKEN" ]]; then
  echo "no token in $SERVER_LOG" >&2
  cat "$SERVER_LOG" >&2
  exit 1
fi

SESSION=$(pnpm dlx agent-browser session id --scope worktree --prefix trellis-ui-e2e)
ab() { pnpm dlx agent-browser --session "$SESSION" "$@"; }

# Waits until nothing matches a selector, such as a dialog's form once it has
# animated out. agent-browser's own `--state` flag is taken by its global
# session-state option, so this counts instead.
wait_gone() {
  for _ in $(seq 1 60); do
    [[ "$(ab get count "$1" 2>/dev/null | tr -dc '0-9')" == "0" ]] && return 0
    sleep 0.2
  done
  echo "still present: $1" >&2
  return 1
}

# The root resolves to the busiest project's overview.
ab open "http://127.0.0.1:$PORT/?token=$TOKEN"
ab wait --url "**/p/REPO"
ab wait --text "Event log"

# Create and edit a card through the board.
ab find role link click --name "Board"
ab wait --text "Seed card"
ab find role button click --name "New card"
ab find label "Card title" fill "Browser-created card"
# The body is the Markdown editor: it takes typing, not a fill, and it covers
# its own label, so focus it by selector rather than by clicking.
ab focus "#card-form .ProseMirror"
ab keyboard type "Created through browser coverage"
ab find role button click --name "Create card"
# The dialog animates out; wait for its form to go before touching the board.
wait_gone "#card-form"
ab wait --text "Browser-created card"

ab find text "Browser-created card" click
ab find role button click --name "Edit"
ab find label "Card title" fill "Browser-edited card"
ab find role button click --name "Save card"
wait_gone "#card-form"
ab wait --text "Browser-edited card"

# Moving a card: the Column control writes the same move a drag does.
ab find role combobox click --name "Column"
ab find role option click --name "In-progress"
moved=""
for _ in $(seq 1 50); do
  if run_trellis card show REPO-3 --json | grep -q '"column":"in-progress"'; then moved=1; break; fi
  sleep 0.1
done
[[ -n "$moved" ]] || { echo "REPO-3 did not move to in-progress" >&2; exit 1; }
ab press Escape

# An independent CLI writer reaches the open board over SSE.
run_trellis card new --title "SSE-created card" --body "created outside the browser" >/dev/null
ab wait --text "SSE-created card"

# The vault: its tree, an entry, and an edit saved in place.
ab find role link click --name "Vault"
ab wait --text "Browser note"
ab find text "Browser note" click
ab wait --text "Seed"
ab find role button click --name "Edit"
ab find label "Title" fill "Browser-edited note"
ab find role button click --name "Save entry"
wait_gone "#entry-form"
ab wait --text "Browser-edited note"

# A new entry starts from the navigator and opens ready to write.
ab find role button click --name "New entry"
ab find label "Title" fill "Browser-created note"
ab find role button click --name "Create entry"
ab wait --url "**/vault/browser-created-note"
ab find role button click --name "Cancel"
wait_gone "#entry-form"

# The seeded claim makes the steal-from-UI path visible and executable.
ab find role link click --name "Board"
ab wait --text "Seed card"
ab find text "Seed card" click
ab wait --text "Claimed by"
ab find role button click --name "Steal the claim"
ab find label "Reason for stealing the claim" fill "browser claim takeover"
ab find role button click --name "Steal it"
ab wait --text "Claimed by you"
ab press Escape

# Settings: a saved change reaches config.yaml, and the templates list loads.
ab find role link click --name "Settings"
ab wait --text "General"
ab find label "List limit" fill "25"
ab find role button click --name "Save"
ab wait --text "Settings saved"
grep -q "ls_limit: 25" "$TRELLIS_HOME_E2E/config.yaml"
ab find role link click --name "Templates"
ab wait --text "decision"

# Search reaches both halves of the product.
ab open "http://127.0.0.1:$PORT/search"
ab find label "Search cards and entries" fill "browser"
ab press Enter
ab wait --text "Browser-edited note"

echo "UI browser smoke passed (board, vault, claim steal, settings, search)."
