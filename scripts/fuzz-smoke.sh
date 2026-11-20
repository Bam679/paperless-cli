#!/bin/sh
# Runs every Fuzz target briefly: $1 is a -fuzztime value (default 3000x
# iterations; fixed counts avoid the deadline flake of time-based runs).
set -eu
t=${1:-3000x}
for pkg in $(go list ./...); do
  for f in $(go test -list '^Fuzz' "$pkg" | grep '^Fuzz' || true); do
    echo "fuzz $pkg $f"
    go test -run '^$' -fuzz "^${f}\$" -fuzztime "$t" "$pkg"
  done
done
