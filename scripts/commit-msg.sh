#!/bin/sh
# Enforce Conventional Commits on the first line of the message file $1.
set -eu
first=$(head -n1 "$1")
case "$first" in Merge\ *|Revert\ *|fixup!\ *|squash!\ *) exit 0 ;; esac
if ! printf '%s' "$first" | grep -Eq '^(feat|fix|docs|chore|refactor|test|ci|build|perf|revert|style)(\([a-z0-9._-]+\))?!?: .{3,}'; then
  echo "commit message must follow Conventional Commits, e.g. 'feat(list): add --due-before'" >&2
  exit 1
fi
