#!/bin/bash
# post-merge-hook.sh — auto-rebuild and deploy st binary after git merge.
#
# Installed as .git/hooks/post-merge via `make install-hook`. Fires automatically
# after every `git merge` (including `git pull --ff-only`) in this as clone.
#
# Two-phase design (I-439 contract: git pull must not block):
#   1. Synchronous fast path: if bin/st already exists, cp it to the workspace
#      immediately. The cp takes milliseconds and does not delay `git pull`.
#   2. Background slow path: run `make install` to stamp a fresh binary from the
#      newly-pulled commit, then re-deploy. Agents starting after the rebuild
#      pick up the post-merge binary; the fast path covers the gap.
#
# Deploy path resolution:
#   Always operates on the MAIN as clone (not a worktree) so `make install`
#   runs in a clean environment and `../theraprac-workspace` resolves correctly.
#   ST_WORKSPACE_ROOT overrides the default for non-standard layouts.

set -euo pipefail

# Resolve the main as clone root from the shared git directory.
# --git-common-dir is relative (".git") in the main clone, absolute in worktrees.
COMMON_GIT="$(git rev-parse --git-common-dir 2>/dev/null || echo ".git")"
if [ "${COMMON_GIT#/}" != "$COMMON_GIT" ]; then
  # Absolute path → we are in a worktree; the main clone is one dir up from .git.
  AS_ROOT="$(dirname "$COMMON_GIT")"
else
  # Relative (".git") → we are in the main clone.
  AS_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"
fi

# Workspace binary target. ST_WORKSPACE_ROOT overrides; default is
# ../theraprac-workspace relative to the agent root (one level up from AS_ROOT).
AGENT_ROOT="$(dirname "$AS_ROOT")"
WS_BIN="${ST_WORKSPACE_ROOT:-$AGENT_ROOT/theraprac-workspace}/bin/st"

# Phase 1: synchronous fast deploy — copy whatever binary currently exists.
# Atomic cp→mv so agents reading theraprac-workspace/bin/st never see a partial
# write. Skipped silently if bin/st doesn't exist yet (fresh-machine first-time
# setup; the background make install below will create it).
if [ -x "$AS_ROOT/bin/st" ] && [ -d "$(dirname "$WS_BIN")" ]; then
  cp "$AS_ROOT/bin/st" "${WS_BIN}.new" 2>/dev/null && \
  mv "${WS_BIN}.new" "$WS_BIN" 2>/dev/null || true
fi

# Phase 2: background make install + re-deploy, stamped with the new commit.
# Keeps git pull responsive. Any agent session starting after this completes
# picks up the correctly-stamped fresh binary.
(
  cd "$AS_ROOT" && \
  make install >/dev/null 2>&1 && \
  [ -x "$AS_ROOT/bin/st" ] && \
  [ -d "$(dirname "$WS_BIN")" ] && \
  cp "$AS_ROOT/bin/st" "${WS_BIN}.new" 2>/dev/null && \
  mv "${WS_BIN}.new" "$WS_BIN" 2>/dev/null
) </dev/null >/dev/null 2>&1 &
disown
