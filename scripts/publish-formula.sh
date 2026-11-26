#!/usr/bin/env bash
# Renders the Homebrew formula for a release and writes it to the tap,
# Formula/vaquero.rb in stazelabs/homebrew-vaquero, through the contents API.
# The release workflow runs it after goreleaser, and the formula workflow runs
# it for a release that is already out. A tap that already holds this
# release's formula is left alone. Run from the repository root:
#
#   GH_TOKEN=... scripts/publish-formula.sh v0.1.3 dist/checksums.txt [TEMPLATE]
#
# GH_TOKEN needs contents read and write on the tap (HOMEBREW_TAP_TOKEN).
# TEMPLATE defaults to packaging/homebrew/vaquero.rb.tmpl.
set -euo pipefail

if [ $# -lt 2 ] || [ $# -gt 3 ]; then
  echo "usage: scripts/publish-formula.sh TAG CHECKSUMS [TEMPLATE]" >&2
  exit 2
fi
tag=$1
sums=$2
tmpl=${3:-packaging/homebrew/vaquero.rb.tmpl}
path=repos/stazelabs/homebrew-vaquero/contents/Formula/vaquero.rb

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
formula=$work/vaquero.rb
go run ./scripts/formula -version "$tag" -checksums "$sums" -template "$tmpl" -o "$formula"

# The file's blob sha, empty when it doesn't exist yet. On an error gh prints
# the error body to stdout (skipping --jq), so only a real 404 counts as
# missing; any other error stops here.
if out=$(gh api "$path" --jq .sha 2>"$work/err"); then
  sha=$out
elif grep -q 'HTTP 404' "$work/err"; then
  sha=
else
  cat "$work/err" >&2
  exit 1
fi
if [ "$sha" = "$(git hash-object "$formula")" ]; then
  echo "The tap already holds the $tag formula; nothing changed."
  exit 0
fi
args=(--method PUT "$path" -f message="vaquero $tag" -f content="$(base64 < "$formula" | tr -d '\n')")
if [ -n "$sha" ]; then args+=(-f sha="$sha"); fi
gh api "${args[@]}" --jq '"Formula/vaquero.rb updated: " + .commit.html_url'
