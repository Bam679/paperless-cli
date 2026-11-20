#!/bin/sh
# Runs every Fuzz target for a short time ($1, default 5s).
set -eu
t=${1:-5s}
for pkg in $(go list ./...); do
  for f in $(go test -list '^Fuzz' "$pkg" | grep '^Fuzz' || true); do
    echo "fuzz $pkg $f"
    go test -run '^$' -fuzz "^${f}\$" -fuzztime "$t" "$pkg"
  done
done
