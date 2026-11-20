#!/bin/sh
# AGENTS.md must stay short and CLAUDE.md must import it.
set -eu
lines=$(wc -l < AGENTS.md | tr -d ' ')
[ "$lines" -lt 60 ] || { echo "AGENTS.md has $lines lines (max 59)"; exit 1; }
grep -qx '@AGENTS.md' CLAUDE.md || { echo "CLAUDE.md must contain the line @AGENTS.md"; exit 1; }
echo "AGENTS.md ok ($lines lines)"
