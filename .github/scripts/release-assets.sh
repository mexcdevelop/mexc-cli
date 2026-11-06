#!/usr/bin/env bash
# .github/scripts/release-assets.sh DIR OUT
#
# Builds the GitHub Release attachments from the npm packages of a release build
# (DIR holds its release-manifest.json and tarballs/):
#
#   mexc_<version>_<platform>.tar.gz   macOS and Linux: mexc, LICENSE, README.md
#   mexc_<version>_<platform>.zip      Windows: mexc.exe, LICENSE, README.md
#   SHA256SUMS                         the sha256 of every archive above, for sha256sum -c
#
# The binaries are taken out of the very tarballs that were published to npm and
# each is checked against the sha256 the manifest recorded when it was packed, so
# a download from the release and an npm install run the same file.
set -euo pipefail

dir="${1:?usage: release-assets.sh DIR OUT}"
out="${2:?usage: release-assets.sh DIR OUT}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
manifest="$dir/release-manifest.json"
[ -f "$manifest" ] || { echo "error: no release manifest at $manifest" >&2; exit 1; }
command -v zip >/dev/null 2>&1 || { echo "error: zip is required" >&2; exit 1; }

mkdir -p "$out"
out="$(cd "$out" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# One line per platform: id, goos, binary sha256, platform tarball file.
platforms="$(node -e '
  const m = require(require("path").resolve(process.argv[1]));
  for (const b of m.binaries) {
    const platform = m.platforms.find((p) => p.id === b.platform);
    const pkg = m.packages.find((p) => p.name.endsWith(`-${b.platform}`));
    console.log([b.platform, platform.goos, b.sha256, pkg.filename].join(" "));
  }
  console.log(`version ${m.version}`);
' "$manifest")"
version="$(printf '%s\n' "$platforms" | sed -n 's/^version //p')"

while read -r id goos sha256 filename; do
  [ "$id" != version ] || continue
  binary="mexc"
  if [ "$goos" = windows ]; then binary="mexc.exe"; fi
  stage="$work/$id"
  mkdir -p "$stage"
  tar -xzf "$dir/tarballs/$filename" -C "$stage" "package/bin/$binary"
  mv "$stage/package/bin/$binary" "$stage/$binary"
  rm -rf "$stage/package"
  actual="$(sha256sum "$stage/$binary" | cut -d' ' -f1)"
  if [ "$actual" != "$sha256" ]; then
    echo "error: $id binary is $actual, the manifest recorded $sha256" >&2
    exit 1
  fi
  cp "$root/LICENSE" "$root/README.md" "$stage/"
  name="mexc_${version}_${id}"
  if [ "$goos" = windows ]; then
    (cd "$stage" && zip -q -X "$out/$name.zip" "$binary" LICENSE README.md)
  else
    tar -czf "$out/$name.tar.gz" -C "$stage" "$binary" LICENSE README.md
  fi
done <<EOF
$platforms
EOF

(cd "$out" && sha256sum mexc_* >SHA256SUMS)
ls -1 "$out" >&2
