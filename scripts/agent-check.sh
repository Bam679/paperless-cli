#!/bin/sh
# Claude Code Stop hook: block finishing while the fast gate fails (exit 2).
cd "$(dirname "$0")/.." || exit 0
if ! out=$(make -s agent-check 2>&1); then
  printf '%s\n' "$out" | tail -n 30 >&2
  echo "make agent-check failed: fix formatting/vet/tests before finishing." >&2
  exit 2
fi
exit 0
