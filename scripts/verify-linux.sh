#!/usr/bin/env bash
# Linux acceptance for the Windows/Linux port.
#
# Runs the real binary in a container and checks the capabilities the port
# actually changed on Linux. Usage, from the repo root:
#
#   bash verify-linux.sh                  # docker, ubuntu + alpine, host arch
#   RUNTIME=podman bash verify-linux.sh
#   IMAGES="ubuntu:24.04" bash verify-linux.sh
#
# Run on 2026-08-31 against ubuntu:24.04 and alpine:3.20, linux/arm64, with
# colima: 10 checks, 0 failures. The storage checks below need a profile, which
# needs a person at a prompt — see the note where they are.
#
# When a check here fails, suspect the harness as well as the product. This one
# reported four false passes before it was trusted: assertions about the ABSENCE
# of a word pass trivially against a binary that never started, and `pgrep -f
# dbus-daemon` matched its own shell because this script contains that string.
set -uo pipefail

RUNTIME="${RUNTIME:-docker}"
IMAGES="${IMAGES:-ubuntu:24.04 alpine:3.20}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[ -f "$REPO/go.mod" ] || REPO="$(pwd)"

pass=0; fail=0
ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; pass=$((pass+1)); }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$1"; fail=$((fail+1)); }
head() { printf '\n\033[1m%s\033[0m\n' "$1"; }

command -v "$RUNTIME" >/dev/null 2>&1 || { echo "no $RUNTIME on PATH"; exit 2; }

# Staging lives under $HOME on purpose. colima mounts $HOME and /tmp/colima but
# NOT /tmp, so a /tmp staging directory shows up EMPTY inside the container and
# every check fails with "not found" — a harness failure that reads exactly like a
# broken binary. Docker Desktop and podman both mount $HOME too.
STAGE="${STAGE:-$HOME/.cache/mexc-linuxverify}"
mkdir -p "$STAGE"

# The script that runs INSIDE the container. Written once, reused per image.
# Every check is about something this port changed on Linux.
read -r -d '' INNER <<'INNER_EOF'
set -u
B=/w/mexc-${ARCH}
P=0; F=0
ok()  { echo "  PASS $1"; P=$((P+1)); }
bad() { echo "  FAIL $1"; F=$((F+1)); }

export HOME=/tmp/h; mkdir -p "$HOME"

# The proxy env docker injects from ~/.docker/config.json points at 127.0.0.1,
# which inside a container is the container itself. Cleared so anything that does
# reach the network can.
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY

# C1 — the binary starts and reports a version.
RAN=0
if v=$("$B" --version 2>&1) && [ -n "$v" ]; then ok "C1 --version: $v"; RAN=1; else bad "C1 --version: $v"; fi

# doctor must be machine-readable and must NOT call a file key a downgrade.
# On Linux the file IS the only key store; a permanent warning about a state
# nobody can change is what commit 4579181 removed.
d=$("$B" doctor 2>/dev/null)
if echo "$d" | grep -q '"ok"'; then
  ok "doctor emits JSON on stdout"
  # Only meaningful once doctor actually produced output. Asserting the ABSENCE of
  # a word passes trivially against an empty string, which is how a check like
  # this reports success for a binary that never started.
  if echo "$d" | grep -qi "downgraded"; then
    bad "doctor still calls the Linux file key a downgrade"
  else
    ok "doctor does not report a downgrade"
  fi
else
  bad "doctor JSON: $(echo "$d" | head -c 200)"
  bad "downgrade wording: SKIPPED, doctor produced no output"
fi

# C3/C4/C5 — enter a credential, read it back, inspect the key at rest.
#
# The prompt reads the controlling terminal by design, so automation has to
# manufacture one. script(1) does exactly that, and tty.go documents this as
# possible; using it to VERIFY the mechanism is not defeating it.
# C3/C4/C5 are NOT automated here, on purpose.
#
# The credential prompt reads the controlling terminal because it is designed to
# require a person at one. Driving it from a script means manufacturing a pty and
# doing expect-style interaction, and that proved genuinely flaky: the same expect
# script succeeded once and then hung repeatedly. The program emits terminal
# queries (OSC 11 background colour, CSI 6n cursor position) and then switches the
# tty to raw mode for the secret via term.ReadPassword — sending into that window
# is a race, and a racy check is worse than an honest gap.
#
# So this harness verifies everything that does NOT need a person, and the storage
# checks below run only if a profile is already present. Seed one by hand, once:
#
#   docker run -it --rm -v "$STAGE:/w:ro" -v mexcstate:/root ubuntu:24.04 \
#     /w/mexc-<arch> profile set probe
#
# and then re-run with the same volume so these checks have something to inspect.
PF="$HOME/.mexc/profiles/probe"
K="$HOME/.mexc/profile-key.key"
if [ -f "$PF" ]; then
  "$B" profile show probe >/tmp/show.log 2>&1 && ok "C4 profile show reads it back" \
    || bad "C4 profile show: $(head -c 250 /tmp/show.log)"
  grep -qi "secret" /tmp/show.log && ok "profile show mentions the secret field" \
    || bad "profile show output unexpected"

  if [ -f "$K" ]; then
    sz=$(wc -c <"$K" | tr -d " ")
    # 32 raw bytes proves two things at once: Linux stores the key unwrapped, and
    # the Windows DPAPI wrap did not leak onto this platform.
    [ "$sz" = 32 ] && ok "C5 key is 32 raw bytes (unwrapped; the wrap did not leak here)" \
                   || bad "C5 key is $sz bytes, expected 32"
    [ "$(stat -c %a "$K")" = 600 ] && ok "C5 key mode 0600" || bad "C5 key mode $(stat -c %a "$K")"
  else
    bad "C5 a profile exists but no key file does"
  fi

  [ "$(stat -c %a "$PF")" = 600 ] && ok "C5 profile mode 0600" || bad "C5 profile mode $(stat -c %a "$PF")"

  # Permission checks DO run on Linux (modeBitsMeaningful = true) — the branch
  # Windows skips, so it has to be exercised where it is real.
  chmod 0644 "$PF"
  "$B" profile show probe >/dev/null 2>&1 && bad "a world-readable profile was still loaded" \
                                          || ok "insecure mode refused on Linux"
  chmod 0600 "$PF"

  # Only with a credential stored is the key backend known, so the wording commit
  # 16cc5e2 changed becomes reachable. Before that the backend is BackendNone.
  d2=$("$B" doctor 2>/dev/null)
  echo "$d2" | grep -qi "only key store this" \
    && ok "doctor calls the file this platform's only key store" \
    || bad "doctor wording: $(echo "$d2" | grep -i -A2 credential | head -c 250)"
else
  echo "  SKIP C3/C4/C5: no profile present — seed one interactively (see the note in this script)"
fi

# No D-Bus anywhere. Commit 4579181 stopped asking the OS keyring on Linux, which
# also removed the autolaunch that could leave an orphan dbus-daemon behind.
if [ "${RAN:-0}" = 1 ]; then
  # -x, not -f: -f matches the full command line, and THIS script contains the
  # string "dbus-daemon", so it matched its own shell (PID 1) and reported a
  # daemon that was never there. busybox pgrep got it wrong the other way.
  if pgrep -x dbus-daemon >/dev/null 2>&1 || pgrep -x dbus-broker >/dev/null 2>&1; then
    bad "a dbus-daemon is running; the keyring path should never be reached on Linux"
  else
    ok "no dbus-daemon spawned"
  fi
else
  bad "dbus check: SKIPPED, the binary never ran"
fi

# C6 — THE AGENT'S SHAPE: a controlling terminal exists while the caller's own
# streams are not on it. This is what hung on Windows, and it is not a Windows
# bug: /dev/tty resolves for any process that still has a controlling terminal,
# and a child an agent shells out to keeps its parent's. The device therefore
# opens for a caller nobody is attending, which is why the gate asks about stdio
# FIRST (pkg/console.StdioIsTerminal). Before that gate, both commands below
# prompted onto a terminal the caller does not own and blocked forever.
#
# script(1) manufactures the controlling terminal. That is the mechanism tty.go
# documents as impossible to tell from a person's — used here to VERIFY the gate,
# not to defeat it: the point is that a real terminal IS present and the answer
# must still be "nobody is attending".
#
# Inside it each command's stdin is /dev/null and its stdout a regular file. A
# file and a pipe are the same answer to IsTerminal, and what this check needs
# from script is only that /dev/tty opens.
#
# The command goes into a generated script rather than through script -c "...":
# nothing here should depend on quoting surviving three levels of shell. This
# harness has already reported four false passes; a quoting bug would be a fifth.
#
# `timeout` IS the assertion. A hang is the failure being tested for, so it must
# be reported as one instead of stalling the harness — 124 is timeout's own code,
# and an empty code file means the same thing.
if command -v script >/dev/null 2>&1; then
  # C6a — `profile set` needs no credentials, so this half always runs. Expect
  # interactive_required (exit 11): stand aside and let the user act.
  cat >/tmp/c6a.sh <<C6A
#!/bin/sh
"$B" profile set __c6_probe__ </dev/null >/tmp/c6a.out 2>&1
echo \$? >/tmp/c6a.code
C6A
  chmod +x /tmp/c6a.sh
  rm -f /tmp/c6a.code /tmp/c6a.out
  timeout 25 script -q -c /tmp/c6a.sh /dev/null >/dev/null 2>&1
  c=$(cat /tmp/c6a.code 2>/dev/null)
  case "$c" in
    11)  ok "C6a profile set refuses with interactive_required (exit 11) under an inherited tty" ;;
    "")  bad "C6a profile set HUNG under an inherited tty (no exit code recorded) — the stdio gate is gone" ;;
    124) bad "C6a profile set HUNG under an inherited tty (timeout) — the stdio gate is gone" ;;
    *)   bad "C6a profile set exit $c, want 11: $(head -c 200 /tmp/c6a.out 2>/dev/null)" ;;
  esac
  # And it must not have created anything. A refusal that half-succeeded is worse
  # than a hang, because nothing later would look wrong.
  if [ -f "$HOME/.mexc/profiles/__c6_probe__" ]; then
    bad "C6a a refused profile set still wrote a profile"
  else
    ok "C6a refused profile set wrote nothing"
  fi

  # C6b — the write-confirmation gate. Needs credentials: the precheck refuses an
  # uncredentialed write BEFORE confirmation (by design — nobody is asked to
  # confirm what provably cannot run), which would mask what this checks. So it
  # runs only with the seeded profile the C3/C4/C5 block describes.
  if [ -f "$PF" ]; then
    cat >/tmp/c6b.sh <<C6B
#!/bin/sh
"$B" activity participate register --data '{"linkFlag":"__c6_probe__"}' </dev/null >/tmp/c6b.out 2>&1
echo \$? >/tmp/c6b.code
C6B
    chmod +x /tmp/c6b.sh
    rm -f /tmp/c6b.code /tmp/c6b.out
    timeout 25 script -q -c /tmp/c6b.sh /dev/null >/dev/null 2>&1
    c=$(cat /tmp/c6b.code 2>/dev/null)
    case "$c" in
      10)  ok "C6b a write refuses with confirmation_required (exit 10) under an inherited tty" ;;
      "")  bad "C6b the write HUNG under an inherited tty (no exit code recorded) — the stdio gate is gone" ;;
      124) bad "C6b the write HUNG under an inherited tty (timeout) — the stdio gate is gone" ;;
      *)   bad "C6b write exit $c, want 10: $(head -c 200 /tmp/c6b.out 2>/dev/null)" ;;
    esac
    if grep -q confirmation_required /tmp/c6b.out 2>/dev/null; then
      ok "C6b the refusal is the relayable confirmation_required envelope"
    else
      bad "C6b refusal payload: $(head -c 200 /tmp/c6b.out 2>/dev/null)"
    fi
  else
    echo "  SKIP C6b: no profile present — an uncredentialed write stops at the precheck, before the gate"
  fi
else
  echo "  SKIP C6: script(1) is absent, so no controlling terminal can be manufactured here"
fi

# C2 offline — request planning works without network.
if "$B" spot marketv2 ticker --symbol BTC_USDT --dry-run >/tmp/dry.log 2>&1; then
  ok "C2 --dry-run plans a request offline"
else
  bad "C2 --dry-run: $(head -c 200 /tmp/dry.log)"
fi

echo "INNER_RESULT $P $F"
INNER_EOF

for img in $IMAGES; do
  for arch in ${ARCHES:-amd64 arm64}; do
    head "$img  linux/$arch"
    bin="$STAGE/mexc-$arch"
    ( cd "$REPO" && GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -o "$bin" ./cmd/mexc ) || {
      bad "cross-compile failed"; continue; }

    out=$("$RUNTIME" run --rm --platform "linux/$arch" \
            -v "$STAGE:/w:ro" "$img" \
            sh -c "ARCH=$arch; $INNER" 2>&1)
    echo "$out" | grep -E '^  (PASS|FAIL|SKIP)' | sed 's/^  PASS/  ✓/; s/^  FAIL/  ✗/'
    r=$(echo "$out" | grep '^INNER_RESULT' | tail -1)
    if [ -z "$r" ]; then
      bad "container produced no result line — harness or image problem"
      echo "$out" | tail -5 | sed 's/^/      /'
    else
      set -- $r; pass=$((pass+$2)); fail=$((fail+$3))
    fi
  done
done

head "Total"
echo "  passed $pass    failed $fail"
[ "$fail" -eq 0 ]
