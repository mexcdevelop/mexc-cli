#!/usr/bin/env bash
# .github/scripts/publish.sh DIR
#
# Publishes the npm packages a release build left in DIR (its
# release-manifest.json and tarballs/) to the registry npm is configured for,
# under the `latest` dist-tag, then tags the main package release-<version>.
#
# Platform packages go first and the main package last: the main package pins
# each platform package to this exact version, so a run that fails part-way can
# only leave platform packages behind, which nothing installs on its own.
#
# Re-running is safe. A package already on the registry at this version is
# skipped when its integrity matches the tarball here, which is what a repeated
# run of the same release looks like. A mismatch means the version was published
# from somewhere else, and the run stops rather than mixing the two.
#
# Authentication is npm's business: in the release workflow it is trusted
# publishing (OIDC), so no token is read or needed here.
set -euo pipefail

dir="${1:?usage: publish.sh DIR}"
manifest="$dir/release-manifest.json"
[ -f "$manifest" ] || { echo "error: no release manifest at $manifest" >&2; exit 1; }

# One line per package, main last: name, version, tarball file, integrity.
packages="$(node -e '
  const m = require(require("path").resolve(process.argv[1]));
  const ordered = [...m.packages].sort((a, b) => (a.name === m.product) - (b.name === m.product));
  for (const p of ordered) console.log([p.name, p.version, p.filename, p.integrity].join(" "));
' "$manifest")"

# wait_for_integrity SPEC INTEGRITY: true once the registry serves SPEC with
# exactly INTEGRITY, polling for up to a minute.
wait_for_integrity() {
  local spec="$1" want="$2" attempt got
  for attempt in 1 2 3 4 5 6; do
    sleep 10
    got="$(npm view "$spec" dist.integrity --prefer-online 2>/dev/null || true)"
    [ -z "$got" ] && continue
    [ "$got" = "$want" ] && return 0
    echo "error: $spec is on the registry with different content ($got)" >&2
    return 1
  done
  return 1
}

published=0
skipped=0
while read -r name version filename integrity; do
  [ -n "$name" ] || continue
  existing="$(npm view "$name@$version" dist.integrity --prefer-online 2>/dev/null || true)"
  if [ -n "$existing" ]; then
    if [ "$existing" != "$integrity" ]; then
      echo "error: $name@$version is already on the registry with different content" >&2
      echo "  registry: $existing" >&2
      echo "  this run: $integrity" >&2
      echo "  the version was published from somewhere else; release under the next version" >&2
      exit 1
    fi
    echo "skip $name@$version (already published, same content)" >&2
    skipped=$((skipped + 1))
    continue
  fi
  if npm publish "$dir/tarballs/$filename" --tag latest --access public; then
    published=$((published + 1))
    continue
  fi
  # The registry answers reads from a cache that trails its writes by seconds,
  # so a re-run that starts right after a successful one can find nothing,
  # publish, and be refused with a conflict over the version that is already
  # there. Wait for the read side to catch up before deciding it was a failure.
  if wait_for_integrity "$name@$version" "$integrity"; then
    echo "skip $name@$version (published by an earlier run, same content)" >&2
    skipped=$((skipped + 1))
    continue
  fi
  echo "error: could not publish $name@$version" >&2
  exit 1
done <<EOF
$packages
EOF

echo "published $published, skipped $skipped" >&2

# The main package also carries a release-<version> dist-tag next to latest,
# the convention of the versions published before this workflow. The platform
# packages never had one. Adding a dist-tag through trusted publishing needs
# "Allow npm dist-tag" on the main package's trusted publisher, and npm 11.21.0
# or later. It runs on every attempt, so a re-run repairs a tag that failed
# after the publish; the retries cover the registry's read lag on a version
# published seconds ago.
read -r product version <<EOF
$(node -e '
  const m = require(require("path").resolve(process.argv[1]));
  process.stdout.write(`${m.product} ${m.version}`);
' "$manifest")
EOF
release_tag="release-$version"
for attempt in 1 2 3 4 5 6; do
  if npm dist-tag add "$product@$version" "$release_tag"; then
    echo "tagged $product@$version $release_tag" >&2
    exit 0
  fi
  [ "$attempt" = 6 ] && break
  sleep 10
done
echo "error: could not add the $release_tag dist-tag to $product@$version" >&2
echo "  check that the trusted publisher of $product allows npm dist-tag" >&2
exit 1
