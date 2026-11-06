# Windows acceptance for the Windows/Linux port.
#
# Runs the real binary on this machine and checks the capabilities the port
# actually changed on Windows. Usage, from the repo root:
#
#   powershell -ExecutionPolicy Bypass -File scripts\verify-windows.ps1
#   powershell -ExecutionPolicy Bypass -File scripts\verify-windows.ps1 -Bin .\mexc.exe
#
# WHY POWERSHELL AND NOT BASH. The counterpart for Linux (verify-linux.sh) runs
# in containers because the platform is not the one this is developed on. Here
# the opposite holds: the machine IS the platform, and the surface the port
# changed is PowerShell's own -- an execution policy that blocks the npm shim, a
# shell that consumes the inner quotes of a JSON argument, a prompt that has to
# refuse instead of hanging. A bash harness under Git Bash reproduces none of
# those: it does not mangle the quotes and does not consult the policy.
#
# When a check here fails, suspect the harness as well as the product. The Linux
# counterpart reported four false passes before it was trusted, and the same two
# traps apply here: an assertion about the ABSENCE of a word passes trivially
# against a binary that never started, and a check that shells out has to prove
# it did not simply time out.
#
# The storage checks need a profile, which needs a person at a prompt. See the
# note where they are.

[CmdletBinding()]
param(
  # Defaults to the binary the build script leaves in the repo root.
  [string]$Bin = ''
)

$ErrorActionPreference = 'Continue'

$script:Passed = 0
$script:Failed = 0

function Write-Head([string]$Text) {
  Write-Host ''
  Write-Host $Text -ForegroundColor White
}
function Write-Ok([string]$Text) {
  Write-Host "  [PASS] $Text" -ForegroundColor Green
  $script:Passed = $script:Passed + 1
}
function Write-Bad([string]$Text) {
  Write-Host "  [FAIL] $Text" -ForegroundColor Red
  $script:Failed = $script:Failed + 1
}
function Write-Skip([string]$Text) {
  Write-Host "  [SKIP] $Text" -ForegroundColor Yellow
}
function Clip([string]$Text, [int]$Max) {
  if ($null -eq $Text) { return '' }
  $t = $Text.Trim()
  if ($t.Length -le $Max) { return $t }
  return $t.Substring(0, $Max)
}

$repo = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
if (-not (Test-Path (Join-Path $repo 'go.mod'))) { $repo = (Get-Location).Path }

if ($Bin -eq '') { $Bin = Join-Path $repo 'mexc.exe' }
if (-not (Test-Path $Bin)) {
  Write-Host "no binary at $Bin -- build one first (bash scripts/build.sh), or pass -Bin <path>" -ForegroundColor Red
  exit 2
}
$Bin = (Resolve-Path $Bin).Path

# Invoke-Bin runs the binary UNATTENDED and never blocks forever.
#
# Unattended is the shape that matters. stdin comes from an empty file and both
# output streams are files, so a console still exists -- this process has one, and
# a child an agent shells out to keeps its parent's -- while the caller's own
# streams are not on it. That is precisely the state that used to hang, and the
# gate under test asks about stdio rather than about the console.
#
# The timeout IS an assertion: a hang is the failure being tested for, so it must
# be reported as one instead of stalling the harness. Code -1 stands for "never
# exited".
function Invoke-Bin {
  param([string[]]$Arguments, [int]$TimeoutMs = 25000)

  $dir = Join-Path ([System.IO.Path]::GetTempPath()) ('mexc-verify-' + [guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Path $dir | Out-Null
  $inFile = Join-Path $dir 'in'
  $outFile = Join-Path $dir 'out'
  $errFile = Join-Path $dir 'err'
  New-Item -ItemType File -Path $inFile | Out-Null

  $proc = Start-Process -FilePath $Bin -ArgumentList $Arguments -NoNewWindow -PassThru -RedirectStandardInput $inFile -RedirectStandardOutput $outFile -RedirectStandardError $errFile
  # Touching Handle is NOT decoration. Start-Process -PassThru hands back a
  # Process whose exit code is unreadable once it has exited -- $proc.ExitCode is
  # empty, and a parameterless WaitForExit does not help. Reading Handle first
  # makes .NET retain it, after which ExitCode is there.
  #
  # This harness reported three false failures before that was found: every check
  # comparing against an exit code saw '' and called a correct refusal wrong.
  # A blank code must never be read as a number, which is why -1 below is
  # explicit and separate.
  $null = $proc.Handle
  $exited = $proc.WaitForExit($TimeoutMs)
  if (-not $exited) {
    try { $proc.Kill() } catch { }
    $code = -1
  } else {
    $code = $proc.ExitCode
  }
  $out = ''
  $err = ''
  if (Test-Path $outFile) { $out = [string](Get-Content $outFile -Raw) }
  if (Test-Path $errFile) { $err = [string](Get-Content $errFile -Raw) }
  Remove-Item $dir -Recurse -Force -ErrorAction SilentlyContinue
  return [pscustomobject]@{ Code = $code; Out = $out; Err = $err; All = ($out + $err) }
}

Write-Head 'mexc Windows acceptance'
Write-Host "  binary: $Bin"
Write-Host "  os:     $([System.Environment]::OSVersion.VersionString)"
Write-Host "  shell:  PowerShell $($PSVersionTable.PSVersion)"

# --------------------------------------------------------------- W1: it starts
$r = Invoke-Bin @('--version')
if ($r.Code -eq 0 -and $r.All.Trim() -ne '') {
  Write-Ok ('W1 --version: ' + (Clip $r.All 120))
} else {
  Write-Bad ("W1 --version exit $($r.Code): " + (Clip $r.All 200))
}

# The .exe suffix is not cosmetic: `go build -o mexc` under GOOS=windows produces
# a file literally named "mexc", which Windows cannot execute. See build.sh.
if ($Bin.ToLower().EndsWith('.exe')) {
  Write-Ok 'W1 the binary carries the .exe suffix Windows needs in order to execute it'
} else {
  Write-Bad "W1 the binary is named $Bin -- Windows cannot execute a file without the extension"
}

# ----------------------------------------------------------------- W2: doctor
$doctor = ''
$r = Invoke-Bin @('doctor')
if ($r.Out -match '"ok"') {
  Write-Ok 'W2 doctor emits JSON on stdout'
  $doctor = $r.Out
} else {
  Write-Bad ('W2 doctor JSON: ' + (Clip $r.All 200))
}

# Everything below reads $doctor. Guarded, because asserting the presence OR the
# absence of a word in an empty string is not a check -- it is a coin that always
# lands the same way.
if ($doctor -ne '') {
  # a425df8: where the OS synthesises mode bits the product does not check them,
  # and doctor is the one caller reporting to a human -- so it has to say the
  # check did not run, rather than print a number derived from a read-only
  # attribute as though it had answered "can another user write here".
  if ($doctor -match 'permissions not checked') {
    Write-Ok 'W3 doctor states the mode check did not run here, instead of printing a mode'
  } else {
    Write-Bad 'W3 doctor does not state that the mode check was skipped on this platform'
  }
  if ($doctor -match 'mode 0(777|666)') {
    Write-Bad 'W3 doctor prints a synthesised mode as though it had been checked'
  } else {
    Write-Ok 'W3 doctor prints no synthesised mode number'
  }

  # 564e563: a policy that blocks the npm shim is worth reporting, because the
  # symptom -- install looks fine, command does nothing -- points nowhere near it.
  if ($doctor -match 'powershell_policy') {
    Write-Ok 'W4 doctor reports the PowerShell execution policy check'
    Write-Host "         (CurrentUser $(Get-ExecutionPolicy -Scope CurrentUser), LocalMachine $(Get-ExecutionPolicy -Scope LocalMachine))"
  } else {
    Write-Bad 'W4 doctor has no powershell_policy check'
  }

  # 16cc5e2 + 6962b3f: no OS keyring is consulted off macOS, so the local file is
  # the only key store -- and it must not be described as a downgrade from one.
  # This is the counterpart of the Linux harness's dbus check.
  if ($doctor -match 'keyring') {
    Write-Bad 'W5 doctor mentions a keyring on a platform that consults none'
  } else {
    Write-Ok 'W5 doctor does not mention an OS keyring'
  }
  if ($doctor -match 'downgrade') {
    Write-Bad 'W5 doctor calls the local key file a downgrade'
  } else {
    Write-Ok 'W5 doctor does not call the local key file a downgrade'
  }
} else {
  Write-Skip 'W3/W4/W5: doctor produced no output, so its wording cannot be judged'
}

# ---------------------------------------- W6: state lives under the user profile
# ccc2629: os.UserHomeDir reads USERPROFILE here, not HOME. A test or harness that
# isolates only HOME isolates nothing on this platform.
$stateDir = Join-Path $env:USERPROFILE '.mexc'
# Matched against the JSON-ESCAPED form: doctor emits JSON, so every separator in
# a Windows path arrives doubled. Matching the raw path finds nothing and reads as
# "the home directory resolved wrongly" -- which is what it reported before this.
$stateDirJSON = $stateDir.Replace('\', '\\')
if ($doctor -match [regex]::Escape($stateDirJSON)) {
  Write-Ok "W6 state resolves under the user profile ($stateDir)"
} else {
  Write-Bad "W6 doctor does not name $stateDir -- check which variable resolves the home directory"
}

# ------------------------------------------ W7: the prompt refuses, never hangs
Write-Head 'the unattended-caller gate'
# a2f8005 + dea8caf + 90b3f88. A console exists for this process, and a child an
# agent shells out to keeps it -- so "is there a console" is the wrong question,
# and answering it is what hung. Expect interactive_required (exit 11): stand
# aside, let the user act, retry the original command unchanged.
$probe = '__w7_probe__'
$probePath = Join-Path $stateDir "profiles\$probe"
$r = Invoke-Bin @('profile', 'set', $probe)
if ($r.Code -eq 11) {
  Write-Ok 'W7 profile set refuses with interactive_required (exit 11) when nobody is attending'
} elseif ($r.Code -eq -1) {
  Write-Bad 'W7 profile set HUNG (never exited) -- the stdio gate is gone'
} else {
  Write-Bad ("W7 profile set exit $($r.Code), want 11: " + (Clip $r.All 200))
}
if ($r.All -match 'interactive_required') {
  Write-Ok 'W7 the refusal is the relayable interactive_required envelope'
} else {
  Write-Bad ('W7 refusal payload: ' + (Clip $r.All 200))
}
# A refusal that half-succeeded is worse than a hang, because nothing later would
# look wrong.
if (Test-Path $probePath) {
  Write-Bad 'W7 a refused profile set still wrote a profile'
  Remove-Item $probePath -Force -ErrorAction SilentlyContinue
} else {
  Write-Ok 'W7 the refused profile set wrote nothing'
}

# ------------------------------------------------------ W8/W9: the --data surfaces
Write-Head 'the argument surfaces PowerShell changes'
# c140851 + ddf396d. PowerShell and cmd.exe both consume the inner double quotes
# of '{"a":1}', so the process receives {a:1}. The product must not answer with a
# schema complaint that sends the caller back to re-quote; it has to name the
# cause and route to a form that carries no quoting at all.
#
# This check can only exist in PowerShell: under Git Bash the same argument
# arrives intact and the branch is never reached.
$mangled = & $Bin activity online list --data '{"limit":3}' 2>&1 | Out-String
if ($mangled -match 'no double quotes at all') {
  Write-Ok 'W8 a quote-stripped body is diagnosed as a shell artefact, not as bad JSON'
} else {
  Write-Bad ('W8 quote-stripped body: ' + (Clip $mangled 220))
}
if ($mangled -match '--data @' -and $mangled -match '--data -') {
  Write-Ok 'W8 the hint offers both quoting-free forms (@<path> and stdin)'
} else {
  Write-Bad 'W8 the hint does not name both quoting-free forms'
}

$bodyFile = Join-Path ([System.IO.Path]::GetTempPath()) 'mexc-verify-body.json'
Set-Content -Path $bodyFile -Value '{"limit":3}' -Encoding utf8
$fromFile = & $Bin activity online list --data "@$bodyFile" --dry-run 2>&1 | Out-String
if ($fromFile -match '"body_checked":\s*true') {
  Write-Ok 'W9 --data @<path> delivers a validated body'
} else {
  Write-Bad ('W9 --data @<path>: ' + (Clip $fromFile 220))
}
$fromStdin = '{"limit":3}' | & $Bin activity online list --data - --dry-run 2>&1 | Out-String
if ($fromStdin -match '"body_checked":\s*true') {
  Write-Ok 'W9 --data - reads the body from stdin'
} else {
  Write-Bad ('W9 --data -: ' + (Clip $fromStdin 220))
}
Remove-Item $bodyFile -Force -ErrorAction SilentlyContinue

# A BOM is the normal case here, not an edge one: PowerShell 5.1's
# `Set-Content -Encoding utf8` writes EF BB BF, so the file W9 just used had one.
# Asserted explicitly because encoding/json rejects a BOM with "invalid character
# '\ufeff'", which names nothing a user can act on.
$bomFile = Join-Path ([System.IO.Path]::GetTempPath()) 'mexc-verify-bom.json'
[System.IO.File]::WriteAllBytes($bomFile, ([byte[]](0xEF, 0xBB, 0xBF) + [System.Text.Encoding]::UTF8.GetBytes('{"limit":3}')))
$fromBOM = & $Bin activity online list --data "@$bomFile" --dry-run 2>&1 | Out-String
if ($fromBOM -match '"body_checked":\s*true') {
  Write-Ok 'W9 a UTF-8 BOM on the body file is tolerated, not reported as bad JSON'
} else {
  Write-Bad ('W9 BOM body: ' + (Clip $fromBOM 220))
}
Remove-Item $bomFile -Force -ErrorAction SilentlyContinue

# UTF-16 is what PowerShell's `>` and Out-File produce by default in some
# configurations, so a body file arriving as UTF-16 is a routine mistake here and
# has to be named as one. Unlike a BOM it cannot be shrugged off: every second
# byte is NUL.
$u16File = Join-Path ([System.IO.Path]::GetTempPath()) 'mexc-verify-u16.json'
[System.IO.File]::WriteAllBytes($u16File, ([byte[]](0xFF, 0xFE) + [System.Text.Encoding]::Unicode.GetBytes('{"limit":3}')))
$fromU16 = & $Bin activity online list --data "@$u16File" --dry-run 2>&1 | Out-String
if ($fromU16 -match 'is UTF-16' -and $fromU16 -match 'must be UTF-8') {
  Write-Ok 'W9 a UTF-16 body file is diagnosed as an encoding problem'
  if ($fromU16 -match 'WriteAllText' -or $fromU16 -match 'Encoding utf8') {
    Write-Ok 'W9 the encoding hint names a PowerShell way to rewrite it'
  } else {
    Write-Bad 'W9 the encoding hint gives no way to fix the file'
  }
} else {
  Write-Bad ('W9 UTF-16 body: ' + (Clip $fromU16 220))
}
Remove-Item $u16File -Force -ErrorAction SilentlyContinue

# The third encoding, and the one most likely to be hit on THIS platform: a
# legacy code page carries no byte-order mark, so nothing can spot it by its
# first bytes and it travels to the JSON parser. Notepad's "ANSI" save is
# GB18030 on a Chinese-locale Windows, and Set-Content without -Encoding writes
# the system code page -- both defaults, not mistakes a user has to go looking
# for. Measured before this was handled: the answer was `json: unknown field
# "<mojibake>"` pointing at `mexc schema`, i.e. at the field names.
$gbkFile = Join-Path ([System.IO.Path]::GetTempPath()) 'mexc-verify-gbk.json'
[System.IO.File]::WriteAllBytes($gbkFile, [byte[]](0x7B, 0x22, 0xB1, 0xB8, 0xD7, 0xA2, 0x22, 0x3A, 0x22, 0xD6, 0xB5, 0x22, 0x7D))
$fromGBK = & $Bin activity online list --data "@$gbkFile" --dry-run 2>&1 | Out-String
if ($fromGBK -match 'is not UTF-8' -and $fromGBK -match 'offset') {
  Write-Ok 'W9 a legacy code page body is diagnosed as an encoding problem, with the offending byte located'
  if ($fromGBK -match 'mexc schema') {
    Write-Bad 'W9 the hint still sends the caller to the schema, which is the wrong direction'
  } else {
    Write-Ok 'W9 the hint is about the encoding, not about the field names'
  }
} else {
  Write-Bad ('W9 legacy code page body: ' + (Clip $fromGBK 220))
}
Remove-Item $gbkFile -Force -ErrorAction SilentlyContinue

# The other half of that check: valid UTF-8 carrying non-ASCII is ordinary and
# must get PAST the encoding gate. A byte-level rejection that also caught this
# would have made the CLI ASCII-only.
#
# Asserted as "not refused for its encoding" rather than "accepted": this
# operation declares no free-form string field, so a body carrying one is
# correctly rejected by the SCHEMA — and asserting acceptance made this check
# fail for a reason that had nothing to do with encoding. What matters is which
# gate answered.
$u8File = Join-Path ([System.IO.Path]::GetTempPath()) 'mexc-verify-u8.json'
[System.IO.File]::WriteAllText($u8File, '{"limit":3,"note":"caf' + [char]0xE9 + '"}', (New-Object System.Text.UTF8Encoding $false))
$fromU8 = & $Bin activity online list --data "@$u8File" --dry-run 2>&1 | Out-String
if ($fromU8 -match 'is not UTF-8') {
  Write-Bad ('W9 valid UTF-8 with non-ASCII was refused as bad encoding: ' + (Clip $fromU8 220))
} else {
  Write-Ok 'W9 valid UTF-8 with non-ASCII passes the encoding gate (the schema may still judge it)'
}
Remove-Item $u8File -Force -ErrorAction SilentlyContinue

# ----------------------------------------------------- W10: planning is offline
$r = Invoke-Bin @('spot', 'marketv2', 'ticker', '--symbol', 'BTC_USDT', '--dry-run')
if ($r.Code -eq 0 -and $r.Out -match '"url"') {
  Write-Ok 'W10 --dry-run plans a request without network'
} else {
  Write-Bad ("W10 --dry-run exit $($r.Code): " + (Clip $r.All 200))
}

# stdout is data, stderr is everything else -- the contract every agent parsing
# this CLI depends on. Worth a check of its own on this platform because there IS
# routine stderr chatter here (a skills-drift notice fires on every invocation
# when the installed docs differ from the build), and if any of it leaked to
# stdout every caller's JSON parse would break.
if ($r.Out -ne '') {
  try {
    $null = $r.Out | ConvertFrom-Json
    Write-Ok 'W10 stdout alone parses as JSON, with stderr chatter kept off it'
  } catch {
    Write-Bad ('W10 stdout is not valid JSON on its own: ' + (Clip $r.Out 200))
  }
} else {
  Write-Bad 'W10 stdout was empty, so its purity cannot be judged'
}

# ------------------------------------------------- W10b: read-only refuses a write
# NOT with a missing --data file: the body is resolved BEFORE the read-only gate,
# so `--data @nonexistent.json` fails on the file and never reaches what this
# checks. That is how this check silently tested nothing at first.
$roBody = Join-Path ([System.IO.Path]::GetTempPath()) 'mexc-verify-ro.json'
Set-Content -Path $roBody -Value '{"linkFlag":"__verify__"}' -Encoding utf8
$r = Invoke-Bin @('--read-only', 'activity', 'participate', 'register', '--data', "@$roBody")
if ($r.All -match 'blocked in --read-only mode') {
  Write-Ok 'W10b --read-only refuses a write, naming the mode as the cause'
} else {
  Write-Bad ("W10b --read-only refusal not produced (exit $($r.Code)): " + (Clip $r.All 200))
}
Remove-Item $roBody -Force -ErrorAction SilentlyContinue

# ------------------------------------------- W10c: the interactive TUI stands aside
# explore drives a full-screen TUI. Without a terminal it must refuse and say
# where to go instead -- a hang here is the same class of bug the W7 gate exists
# for, and this is the other command that could hit it.
$r = Invoke-Bin @('explore')
if ($r.Code -eq -1) {
  Write-Bad 'W10c explore HUNG without a terminal'
} elseif ($r.All -match 'requires a terminal') {
  Write-Ok 'W10c explore refuses without a terminal instead of hanging'
  if ($r.All -match 'mexc schema') {
    Write-Ok 'W10c the refusal points at the machine-readable alternative'
  } else {
    Write-Bad 'W10c the refusal names no alternative for a non-interactive caller'
  }
} else {
  Write-Bad ("W10c explore exit $($r.Code): " + (Clip $r.All 200))
}

# --------------------------------------------- W10d: a home directory with a space
# "C:\Users\John Smith" is an ordinary Windows account, and a path with a space in
# it is where naive quoting breaks. Checked by pointing the state directory at one
# and reading back what the product resolved.
$spaced = Join-Path ([System.IO.Path]::GetTempPath()) 'mexc verify spaced home'
New-Item -ItemType Directory -Path $spaced -Force | Out-Null
# try/finally, not a plain restore: if anything between the two throws, the
# variable stays pointed at the temporary home and EVERY later check silently
# reports against it. That is a harness failure that reads exactly like a broken
# product, which is the trap this script's header warns about.
$savedProfile = $env:USERPROFILE
try {
  $env:USERPROFILE = $spaced
  $r = Invoke-Bin @('doctor')
} finally {
  $env:USERPROFILE = $savedProfile
}
$expected = (Join-Path $spaced '.mexc').Replace('\', '\\')
if ($r.Out -match [regex]::Escape($expected)) {
  Write-Ok 'W10d a home directory containing spaces resolves correctly'
} else {
  Write-Bad ('W10d spaced home not resolved: ' + (Clip $r.Out 240))
}
Remove-Item $spaced -Recurse -Force -ErrorAction SilentlyContinue

# ------------------------------------------------- W10e: the MCP server over stdio
# The MCP surface is how an agent host actually drives this CLI, and it is a
# different transport from the command line: a long-lived process speaking
# JSON-RPC over stdin/stdout. Worth its own check because nothing else here
# exercises stdin staying open.
$r = Invoke-Bin @('mcp', '--help')
if ($r.Code -eq 0 -and $r.All -match 'MCP tools over stdio') {
  Write-Ok 'W10e the mcp subcommand is present and documents its stdio transport'
} else {
  Write-Bad ("W10e mcp --help exit $($r.Code): " + (Clip $r.All 200))
}
# The handshake itself needs a client that keeps stdin open, which is node's job
# here rather than PowerShell's -- driving a duplex pipe from PowerShell means
# managing both streams by hand and is the kind of harness code that produces
# false failures.
$node = Get-Command node -ErrorAction SilentlyContinue
if ($null -ne $node) {
  $driver = Join-Path ([System.IO.Path]::GetTempPath()) 'mexc-mcp-probe.cjs'
  $driverSource = @'
const cp = require("node:child_process");
const p = cp.spawn(process.argv[2], ["mcp", "contract"], { stdio: ["pipe", "pipe", "ignore"] });
let out = "";
p.stdout.on("data", (d) => { out += d; });
const send = (o) => p.stdin.write(JSON.stringify(o) + "\n");
send({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2024-11-05", capabilities: {}, clientInfo: { name: "verify", version: "1" } } });
setTimeout(() => {
  send({ jsonrpc: "2.0", method: "notifications/initialized" });
  send({ jsonrpc: "2.0", id: 2, method: "tools/list" });
}, 400);
setTimeout(() => {
  p.kill();
  let init = false, tools = 0;
  for (const line of out.trim().split("\n").filter(Boolean)) {
    try {
      const j = JSON.parse(line);
      if (j.result && j.result.serverInfo) init = true;
      if (j.result && Array.isArray(j.result.tools)) tools = j.result.tools.length;
    } catch (_) { /* not a frame */ }
  }
  console.log(JSON.stringify({ init, tools }));
}, 2000);
'@
  Set-Content -Path $driver -Value $driverSource -Encoding utf8
  $mcp = & $node.Source $driver $Bin 2>$null | Out-String
  Remove-Item $driver -Force -ErrorAction SilentlyContinue
  $verdict = $null
  try { $verdict = $mcp | ConvertFrom-Json } catch { }
  if ($null -ne $verdict -and $verdict.init -and $verdict.tools -gt 0) {
    Write-Ok "W10e the MCP server completes the handshake and lists $($verdict.tools) tools over stdio"
  } else {
    Write-Bad ('W10e MCP handshake produced no usable result: ' + (Clip $mcp 200))
  }
} else {
  Write-Skip 'W10e: no node on PATH, so the MCP handshake cannot be driven'
}

# --------------------------------------------------------- W11: storage at rest
Write-Head 'credentials at rest'
# NOT AUTOMATED, on purpose, and for the reason the Linux harness gives: the
# prompt reads the console because it is designed to require a person at one.
# Driving it means manufacturing console input and racing the secret's raw-mode
# read, and a racy check is worse than an honest gap.
#
# Seed one by hand, once, in your own terminal:
#
#   .\mexc.exe profile set probe
#
# then re-run this script and these checks have something to inspect.
#
# ANY saved profile will do, and the name is discovered rather than fixed. A
# developer machine already has credentials under its own names, and demanding one
# called "probe" meant these four checks skipped on exactly the machines best able
# to run them. Nothing here writes to the profile: it is read back, and the
# foreign-key case works on a COPY under a temporary home.
$keyFile = Join-Path $stateDir 'profile-key.key'
$profileDir = Join-Path $stateDir 'profiles'
$profileName = ''
if (Test-Path $profileDir) {
  $firstProfile = Get-ChildItem -Path $profileDir -File -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($null -ne $firstProfile) { $profileName = $firstProfile.Name }
}
$profileFile = ''
if ($profileName -ne '') { $profileFile = Join-Path $profileDir $profileName }

if ($profileName -ne '') {
  Write-Host "         (inspecting the saved profile '$profileName')"
  $r = Invoke-Bin @('profile', 'show', $profileName)
  if ($r.Code -eq 0 -and $r.Out -match 'secret') {
    Write-Ok 'W11 profile show reads the credential back through the wrapped key'
  } else {
    Write-Bad ("W11 profile show exit $($r.Code): " + (Clip $r.All 220))
  }

  if (Test-Path $keyFile) {
    $size = (Get-Item $keyFile).Length
    # 6962b3f. The counterpart of the Linux harness's "32 raw bytes": there the
    # file IS the key, here it is a DPAPI blob bound to this user account. A
    # 32-byte file on Windows would mean the wrap silently stopped happening and
    # the key is lying there in the clear.
    if ($size -eq 32) {
      Write-Bad 'W12 the key file is 32 raw bytes -- the DPAPI wrap is not being applied'
    } elseif ($size -gt 32) {
      Write-Ok "W12 the key file is $size bytes, larger than the key it holds (DPAPI-wrapped)"
    } else {
      Write-Bad "W12 the key file is $size bytes, smaller than a key"
    }
    # What is NOT asserted, and why: that another account on this machine cannot
    # unwrap it is DPAPI's guarantee, and one account cannot test it. Stated
    # rather than faked with a weaker check that looks like the real one.
    Write-Skip 'W12 the other-account property is DPAPI-enforced; a single account cannot verify it'
  } else {
    Write-Bad 'W12 a profile exists but no key file does'
  }

  # The branch Windows deliberately does NOT take, named so its absence is a
  # decision on record rather than an oversight. On Linux the harness widens the
  # profile's mode and asserts the read is refused; here the bits are synthesised,
  # every writable file reads 0666, and the product skips that check by design.
  Write-Skip 'W13 the insecure-mode refusal is a Linux-side check; this platform synthesises mode bits (secfile.ModeBitsMeaningful)'

  # 3334c1b: a key that cannot be unwrapped HERE may still be the only working
  # copy where this directory syncs to, so the remedy must never be deletion.
  # Driven by pointing the state directory at a fresh home holding a copied
  # profile and a random key file -- which is what a synced directory looks like
  # when it lands on another machine.
  $foreign = Join-Path ([System.IO.Path]::GetTempPath()) ('mexc-verify-foreign-' + [guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Path (Join-Path $foreign '.mexc\profiles') -Force | Out-Null
  Copy-Item $profileFile (Join-Path $foreign ('.mexc\profiles\' + $profileName)) -Force
  $bytes = New-Object byte[] 262
  (New-Object Random).NextBytes($bytes)
  [System.IO.File]::WriteAllBytes((Join-Path $foreign '.mexc\profile-key.key'), $bytes)
  # try/finally for the reason given at the spaced-home check above.
  $saved = $env:USERPROFILE
  try {
    $env:USERPROFILE = $foreign
    $r = Invoke-Bin @('profile', 'show', $profileName)
  } finally {
    $env:USERPROFILE = $saved
  }
  if ($r.All -match 'do NOT delete' -or $r.All -match 'synced folder') {
    Write-Ok 'W14 an unusable key file is not advised away -- the remedy names the shared-directory case'
  } elseif ($r.All -match 'remove it and save') {
    Write-Bad 'W14 the remedy tells the user to delete a key that may still work elsewhere'
  } else {
    Write-Skip ('W14 foreign-key remedy not identified: ' + (Clip $r.All 200))
  }
  Remove-Item $foreign -Recurse -Force -ErrorAction SilentlyContinue
} else {
  Write-Skip 'W11-W14: no saved profile to inspect -- seed one interactively (see the note in this script)'
}

# ------------------------------------------------ W15: packaging refuses here
Write-Head 'the packaging refusal'
# 6ab6f59. npm pack reads each file's mode from the filesystem and Windows has no
# executable bit to give it, so a POSIX tarball built here installs dead. The
# refusal is the product working correctly; what must NOT appear is a tarball.
$node = Get-Command node -ErrorAction SilentlyContinue
if ($null -ne $node) {
  $tmpOut = Join-Path ([System.IO.Path]::GetTempPath()) ('mexc-pack-' + [guid]::NewGuid().ToString('N'))
  $packArgs = @(
    (Join-Path $repo 'npmkit\package.mjs'),
    '--product', (Join-Path $repo 'npm\product.json'),
    '--output', $tmpOut,
    '--snapshot', '--version', '1.2.3-beta.1',
    '--skip-build', '--binaries-dir', ([System.IO.Path]::GetTempPath())
  )
  $packed = & $node.Source $packArgs 2>&1 | Out-String
  if ($packed -match 'npm packaging cannot run on Windows') {
    Write-Ok 'W15 packaging a POSIX target is refused, naming the host as the cause'
    if ($packed -match 'Cross-compiling for any target') {
      Write-Ok 'W15 the refusal still points at cross-compiling, so it does not read as "Windows unsupported"'
    } else {
      Write-Bad 'W15 the refusal does not mention that cross-compiling works here'
    }
  } else {
    Write-Bad ('W15 packaging was not refused: ' + (Clip $packed 220))
  }
  Remove-Item $tmpOut -Recurse -Force -ErrorAction SilentlyContinue
} else {
  Write-Skip 'W15: no node on PATH, so the packaging refusal cannot be exercised'
}

# ----------------------------------------------------------------------- total
Write-Head 'total'
Write-Host "  pass $script:Passed    fail $script:Failed"
if ($script:Failed -eq 0) { exit 0 } else { exit 1 }
