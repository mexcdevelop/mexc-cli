#!/usr/bin/env bash
# .github/scripts/release-notes.sh VERSION [CHANGELOG]
#
# Prints the CHANGELOG.md section for VERSION (e.g. v0.0.23): every line after
# its "## VERSION" heading up to the next "## " heading, with leading and
# trailing blank lines dropped. Exits 1 when the section is missing or empty.
#
# It is the single source of a release's notes. The GitHub Release body comes
# from it, and so does the body of the commit that publishes the version, so the
# two cannot say different things. A version tag's own annotation is never used:
# it is not part of the published tree and nothing reviews it for that.
set -euo pipefail

version="${1:?usage: release-notes.sh VERSION [CHANGELOG]}"
changelog="${2:-$(cd "$(dirname "$0")/../.." && pwd)/CHANGELOG.md}"

[ -f "$changelog" ] || { echo "error: no changelog at $changelog" >&2; exit 1; }

notes="$(awk -v version="$version" '
  /^## / {
    if (inside) exit
    split($0, words, " ")
    if (words[2] == version) inside = 1
    next
  }
  inside { print }
' "$changelog" | sed -e '/./,$!d' | awk '{ lines[NR] = $0 } /./ { last = NR } END { for (i = 1; i <= last; i++) print lines[i] }')"

if [ -z "$notes" ]; then
  echo "error: $changelog has no \"## $version\" section, or it is empty" >&2
  echo "  write the release notes there before tagging the release" >&2
  exit 1
fi
printf '%s\n' "$notes"
