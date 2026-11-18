#!/bin/sh
# Enforces per-package coverage floors from coverage-floors.txt.
#
# The profile under -coverpkg=./... is large (hundreds of MB), so it is read
# exactly once: one awk pass totals every floored package, then the floors
# are compared. Reading it once per floor took the gate past nine minutes.
set -eu
MOD=$(go list -m)
PROFILE=$(mktemp)
OUT=$(mktemp)
trap 'rm -f "$PROFILE" "$OUT"' EXIT
# Quiet on success; on a failing test, show why (the gate otherwise says nothing).
if ! CGO_ENABLED=0 go test -coverpkg=./... -coverprofile="$PROFILE" ./... >"$OUT" 2>&1; then
  grep -v -e '^ok ' -e 'no test files' -e 'coverage: ' "$OUT" >&2 || true
  echo "cover-check: tests failed under -coverpkg" >&2
  exit 1
fi
# A block is file:range; its statement count is the same in every binary that
# linked it, and it counts as covered if any binary ran it. Only files directly
# in a floored package's directory count (no sub-packages).
awk -v mod="$MOD/" '
  FNR == NR && FILENAME == ARGV[1] {
    if ($0 ~ /^[ \t]*(#|$)/) next
    floor[$1] = $2; order[++n] = $1
    next
  }
  FNR == 1 { next }
  {
    split($1, a, ":"); file = a[1]
    if (index(file, mod) != 1) next
    rel = substr(file, length(mod) + 1)
    slash = 0
    for (i = length(rel); i > 0; i--) if (substr(rel, i, 1) == "/") { slash = i; break }
    if (slash == 0) next
    pkg = substr(rel, 1, slash - 1)
    if (!(pkg in floor)) next
    if (!($1 in stmts)) { stmts[$1] = $2; total[pkg] += $2 }
    if ($3 > 0 && !($1 in hit)) { hit[$1] = 1; covered[pkg] += stmts[$1] }
  }
  END {
    fail = 0
    for (j = 1; j <= n; j++) {
      pkg = order[j]
      if (!(pkg in total) || total[pkg] == 0) {
        printf "cover-check: no statements found for %s\n", pkg; fail = 1; continue
      }
      pct = sprintf("%.1f", 100 * covered[pkg] / total[pkg]) + 0
      if (pct < floor[pkg] + 0) {
        printf "cover-check: FAIL %s %.1f%% is below %s%%\n", pkg, pct, floor[pkg]; fail = 1
      } else {
        printf "cover-check: ok   %s %.1f%% (floor %s%%)\n", pkg, pct, floor[pkg]
      }
    }
    exit fail
  }
' coverage-floors.txt "$PROFILE"
