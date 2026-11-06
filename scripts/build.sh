#!/usr/bin/env bash
# scripts/build.sh — build mexc with version metadata injected from git.
#
# Usage (see `bash scripts/build.sh --help`):
#   bash scripts/build.sh              # host platform            -> ./mexc
#   bash scripts/build.sh --install    # host platform            -> $GOBIN (or ~/go/bin)
#   bash scripts/build.sh --target linux/arm64   # one cross target -> ./dist/bin/<id>/
#   bash scripts/build.sh --target all           # every target the kit can build
#   bash scripts/build.sh --snapshot --version VERSION
#                                      # tagless prerelease npm tarballs -> ./dist/npm/{scoped,unscoped}/
#   bash scripts/build.sh --release [--product scoped|unscoped|all]
#                                      # npm tarballs             -> ./dist/npm/{scoped,unscoped}/
#
# Version resolution: the latest git tag (e.g. v0.1.0), else "dev".
set -euo pipefail

# Builds are intentionally hermetic: dependencies must come from vendor/ and
# the required Go toolchain must already be installed on the build host.
export GOTOOLCHAIN=local
export GOPROXY=off
export GOSUMDB=off

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

# The ldflags-injected build metadata lives in pkg/buildinfo — an EXPORTED path,
# deliberately. `-X` resolves a symbol by import path and silently does nothing
# when the path does not match, so a private path here (internal/cli, where these
# vars used to live) would turn any upstream rename into release binaries that
# quietly report "dev / none / unknown". assert_stamped below is the second half
# of that guard.
PKG="github.com/mexcdevelop/mexc-cli/pkg/buildinfo"
CMD="./cmd/mexc"

VERSION="$(git describe --tags --abbrev=0 2>/dev/null || echo dev)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
# Commit date (deterministic, unlike build wall-clock); falls back to "unknown".
DATE="$(git show -s --format=%cd --date=format:%Y-%m-%dT%H:%M:%SZ HEAD 2>/dev/null || echo unknown)"

LDFLAGS="-s -w \
  -X ${PKG}.Version=${VERSION} \
  -X ${PKG}.Commit=${COMMIT} \
  -X ${PKG}.Date=${DATE}"

# HOST_GOOS / HOST_GOARCH are the platform this script is RUNNING on, which is not
# what `go env GOOS` reports: that honours the GOOS/GOARCH environment variables,
# so under `GOOS=linux bash scripts/build.sh` it answers "linux" and every
# comparison against it concludes the cross-compiled artifact is native. Clearing
# the variables for the query is what makes the answer about the machine.
#
# Measured, not theoretical: before this, `GOOS=linux bash scripts/build.sh` on a
# Mac built a correct Linux binary and then tried to EXECUTE it, and reported
# "version stamping did not take — the -ldflags -X path is probably stale". The
# stamping was fine. A diagnostic that names the wrong cause is worse than none.
HOST_GOOS="$(GOOS= GOARCH= go env GOOS)"
HOST_GOARCH="$(GOOS= GOARCH= go env GOARCH)"

# binary_name is what the artifact must be CALLED for the target to be able to run
# it. Windows needs the executable extension — `go build -o mexc` under
# GOOS=windows produces a file literally named "mexc" (verified; Go does not
# append it), which Windows cannot execute.
#
# npmkit/package.mjs makes the same decision for the packaged artifact. Two places
# deciding one thing is a drift risk, and the answer has to be the same in both:
# the launcher resolves the binary by the name recorded at package time.
binary_name() {
  case "$1" in
  windows) echo "mexc.exe" ;;
  *) echo "mexc" ;;
  esac
}

# assert_stamped verifies the -X injection actually landed. It exists because a
# `-X` naming a symbol the linker cannot resolve is NOT an error: the build
# succeeds and the binary reports its compiled-in defaults. Nothing else in the
# pipeline would notice, so a release artifact would be an anonymous "dev"
# build. Only checked for a host-native binary (a cross-compiled
# one cannot be executed here) and only when git actually gave us a version.
assert_stamped() {
  local out="$1" goos="$2" goarch="$3"
  [ "$goos" = "$HOST_GOOS" ] && [ "$goarch" = "$HOST_GOARCH" ] || return 0
  [ "$VERSION" != "dev" ] || return 0
  local got
  got="$("$out" --version 2>/dev/null || true)"
  case "$got" in
  *"$VERSION"*) ;;
  *)
    echo "✗ version stamping did not take: expected ${VERSION}, got: ${got:-<no output>}" >&2
    echo "  the -ldflags -X path is probably stale — check PKG=${PKG}" >&2
    echo "  (a -X naming an unresolvable symbol fails silently, which is what this catches)" >&2
    return 1
    ;;
  esac
}

build_one() {
  local goos="$1" goarch="$2" out="$3"
  echo "building $out ($goos/$goarch) ${VERSION}"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -mod=vendor -trimpath -ldflags "$LDFLAGS" -o "$out" "$CMD"
  assert_stamped "$out" "$goos" "$goarch"
}

# resolve_python prints a command that RUNS Python, or fails.
#
# `command -v python3` is the wrong question on Windows: the official installer
# ships python.exe and pythonw.exe and NO python3.exe, so the bare name resolves
# to the Microsoft Store's App Execution Alias — a stub that exits 49 and prints
# nothing. Measured here: with a working Python 3.13 installed and first on PATH,
# `python3 -V` still exited 49 with no output, and this script reported
# "cannot read the platform matrix" — a diagnosis pointing at the JSON file,
# which was fine.
#
# So candidates are probed by EXECUTING them, which is the only check that tells
# a stub from an interpreter. `py -3` is the Windows launcher, the normal way to
# reach a Python installed there.
resolve_python() {
  local candidate
  for candidate in python3 python; do
    if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c '' >/dev/null 2>&1; then
      echo "$candidate"
      return 0
    fi
  done
  if command -v py >/dev/null 2>&1 && py -3 -c '' >/dev/null 2>&1; then
    echo "py -3"
    return 0
  fi
  # Last resort: the official per-user install directory.
  #
  # The Windows installer's default is "install for me only", which lands the
  # interpreter here and does NOT put it on PATH unless the "Add python.exe to
  # PATH" box was ticked — a box that is off by default. So a machine can hold a
  # perfectly good Python that none of the names above can reach, while the Store
  # alias cheerfully answers all of them. MEASURED on the machine this was written
  # on: Python 3.13.15 installed at exactly this path, with python3, python and py
  # all failing, and the four check scripts consequently never running locally.
  #
  # The first working interpreter wins, and version does not matter: these scripts
  # import nothing outside the standard library. A path containing a space is
  # skipped rather than returned, because the callers expand this value unquoted
  # in order to split "py -3" — a space would break them in a way far harder to
  # diagnose than "no Python found".
  local base dir
  base="${LOCALAPPDATA:-}"
  if [ -n "$base" ]; then
    base="$(cygpath -u "$base" 2>/dev/null || echo "$base")"
    for dir in "$base"/Programs/Python/Python3*; do
      [ -d "$dir" ] || continue
      case "$dir" in *" "*) continue ;; esac
      if [ -x "$dir/python.exe" ] && "$dir/python.exe" -c '' >/dev/null 2>&1; then
        echo "$dir/python.exe"
        return 0
      fi
    done
  fi
  return 1
}

# build_targets cross-compiles one target, or every target the kit can build.
#
# It reads npmkit/platforms.json — the kit's CAPABILITY — and deliberately NOT
# npm/product.json, which is the separate decision about what gets PUBLISHED. That
# is what lets you produce a binary for a target to test with before any product
# declares it. Conflating the two would make "I cannot publish it yet" mean "I
# cannot build it to check", which is the opposite of useful.
build_targets() {
  local want="$1"
  local matrix="$REPO_ROOT/npmkit/platforms.json"
  [ -f "$matrix" ] || { echo "✗ missing $matrix" >&2; exit 1; }

  # An explicit target and a GOOS/GOARCH in the environment are two answers to one
  # question. Refuse rather than letting one silently win: "I set GOOS and it did
  # nothing" is the class of accident this flag exists to end.
  if [ -n "${GOOS:-}${GOARCH:-}" ]; then
    echo "✗ --target and GOOS/GOARCH cannot both be set; drop one" >&2
    exit 2
  fi

  local py
  py="$(resolve_python)" || {
    echo "✗ no working Python found; it is needed only to read $matrix" >&2
    echo "  (on Windows python3/python may be a 0-byte Microsoft Store alias, not an interpreter." >&2
    echo "   py -3 and %LOCALAPPDATA%\\Programs\\Python\\Python3*\\python.exe are probed too, so if this" >&2
    echo "   still finds nothing, either install Python or turn the alias off:" >&2
    echo "   Settings > Apps > Advanced app settings > App execution aliases)" >&2
    exit 1
  }

  local pairs
  # Unquoted $py on purpose: resolve_python may answer "py -3", a command and its
  # argument, and the word splitting is what separates them.
  # shellcheck disable=SC2086
  pairs="$($py -c '
import json, sys
for p in json.load(open(sys.argv[1])):
    print(p["goos"], p["goarch"], p["id"])
' "$matrix")" || { echo "✗ cannot read the platform matrix" >&2; exit 1; }

  local built=0 goos goarch id dir
  while read -r goos goarch id; do
    [ -n "$goos" ] || continue
    if [ "$want" != all ] && [ "$want" != "$goos/$goarch" ]; then
      continue
    fi
    dir="dist/bin/$id"
    mkdir -p "$dir"
    build_one "$goos" "$goarch" "$dir/$(binary_name "$goos")"
    built=$((built + 1))
  done <<TARGETS
$pairs
TARGETS

  if [ "$built" -eq 0 ]; then
    echo "✗ no such target: $want" >&2
    echo "  the kit can build:" >&2
    echo "$pairs" | while read -r goos goarch _; do echo "    $goos/$goarch" >&2; done
    exit 2
  fi
  echo "done -> dist/bin/ ($built target(s))"
}

package_npm() {
  local command="$1"
  shift
  local mode=""
  case "$command" in
  --snapshot) mode="snapshot" ;;
  --release) mode="release" ;;
  *) echo "error: unsupported npm packaging mode: $command" >&2; exit 2 ;;
  esac

  local version="" variant="all"
  while [ "$#" -gt 0 ]; do
    case "$1" in
    --version)
      [ "$#" -ge 2 ] || { echo "error: --version requires a value" >&2; exit 2; }
      version="$2"
      shift 2
      ;;
    --product)
      [ "$#" -ge 2 ] || { echo "error: --product requires a value: scoped, unscoped or all" >&2; exit 2; }
      variant="$2"
      shift 2
      ;;
    *)
      echo "error: unknown packaging option: $1" >&2
      exit 2
      ;;
    esac
  done
  if [ "$mode" = "snapshot" ]; then
    [ -n "$version" ] || { echo "error: --snapshot requires --version VERSION" >&2; exit 2; }
  elif [ -n "$version" ]; then
    echo "error: --release derives its version from the exact HEAD tag; remove --version" >&2
    exit 2
  fi
  command -v node >/dev/null 2>&1 || { echo "error: Node.js is required for npm packaging" >&2; exit 1; }
  # --product picks which package variant to build. The default builds both,
  # as before; a publisher that ships only one asks for it rather than building
  # and then ignoring the other.
  local -a products=()
  case "$variant" in
  all) products=("npm/product.json:scoped" "npm/product-unscoped.json:unscoped") ;;
  scoped) products=("npm/product.json:scoped") ;;
  unscoped) products=("npm/product-unscoped.json:unscoped") ;;
  *) echo "error: unknown --product: $variant (use scoped, unscoped or all)" >&2; exit 2 ;;
  esac
  local spec product output
  local -a args
  for spec in "${products[@]}"; do
    product="${spec%%:*}"
    output="dist/npm/${spec##*:}"
    args=(
      npmkit/package.mjs
      --product "$product"
      --output "$output"
    )
    if [ "$mode" = "snapshot" ]; then
      args+=(--snapshot --version "$version")
    fi
    node "${args[@]}"
  done
}

case "${1:-}" in
-h | --help)
  cat <<'USAGE'
scripts/build.sh — build mexc with version metadata injected from git.

  bash scripts/build.sh              Build for the host platform       -> ./mexc
  bash scripts/build.sh --install    Build and install for the host    -> $GOBIN (or ~/go/bin)
  bash scripts/build.sh --target GOOS/GOARCH
                                     Cross-compile one target          -> ./dist/bin/<platform-id>/
  bash scripts/build.sh --target all Cross-compile every target the kit can build
  bash scripts/build.sh --snapshot --version VERSION
                                     Build tagless prerelease npm tarballs -> ./dist/npm/{scoped,unscoped}/
  bash scripts/build.sh --release
                                     Build both package variants       -> ./dist/npm/{scoped,unscoped}/

  Either packaging mode also takes --product scoped|unscoped|all (default all)
  to build only one variant: scoped is npm/product.json, unscoped is
  npm/product-unscoped.json.

With no argument the target is the machine this runs on. --target does not change
that default; it only adds a way to ask for another one, and refuses if GOOS or
GOARCH is also set in the environment rather than letting one of them win
silently. A Windows target is named mexc.exe, because Windows cannot execute a
file without the extension and `go build -o mexc` does not add it.

--target reads npmkit/platforms.json, which is what the kit CAN build. That is a
different list from npm/product.json, which is what this repository chooses to
PUBLISH — so a platform can be built for testing before it is shipped. Output
directories use the platform id from that file (linux-x64, win32-x64), matching
the layout the packaging modes produce under dist/npm/*/binaries/.

A packaging mode produces one tarball per platform the product declares, plus a
main package, for each of the two variants. The count is not stated here on
purpose: it follows npm/product.json and a number written down goes stale.

The version string comes from the latest git tag (else "dev") and is stamped
into the binary together with the short commit and the commit date.

An npm snapshot requires an explicit prerelease version, such as
1.2.3-beta.42, but does not require a Git tag. An npm release requires HEAD to
have exactly one valid vMAJOR.MINOR.PATCH[-prerelease] tag and derives the
package version from that tag. Packaging uses the current worktree state, so CI
may create local files before invoking this script. No registry is named anywhere:
the generated packages are registry-agnostic, and publication reads the address
from the publishing environment's own npm configuration. These commands only
create artifacts; they do not publish packages or change tags.
USAGE
  ;;
--install)
  # Installing a cross-compiled binary into the host's GOBIN would put something
  # unrunnable on the PATH, so this one is host-only by construction.
  out="${GOBIN:-$HOME/go/bin}/$(binary_name "$HOST_GOOS")"
  mkdir -p "$(dirname "$out")"
  build_one "$HOST_GOOS" "$HOST_GOARCH" "$out"
  echo "installed -> $out"
  ;;
--snapshot | --release)
  package_npm "$@"
  ;;
--target)
  [ $# -ge 2 ] || { echo "✗ --target needs a value: GOOS/GOARCH, or 'all'" >&2; exit 2; }
  build_targets "$2"
  ;;
"")
  # No argument means the machine this runs on. Unchanged, deliberately: adding
  # --target must not move the default.
  out="./$(binary_name "$HOST_GOOS")"
  build_one "$HOST_GOOS" "$HOST_GOARCH" "$out"
  echo "done -> $out"
  ;;
*)
  echo "✗ unknown argument: $1 (try --help)" >&2
  exit 1
  ;;
esac
