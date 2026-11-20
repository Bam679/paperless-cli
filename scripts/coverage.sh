#!/bin/sh
# Fails if total coverage of the core packages (internal/...) is below $2 %.
set -eu
profile=$1; min=$2
grep -v '/cmd/' "$profile" > "$profile.core"
total=$(go tool cover -func="$profile.core" | awk '/^total:/ {sub("%","",$3); print $3}')
rm -f "$profile.core"
echo "core coverage: ${total}% (min ${min}%)"
awk -v t="$total" -v m="$min" 'BEGIN { exit (t + 0 >= m + 0) ? 0 : 1 }' || { echo "coverage below ${min}%"; exit 1; }
