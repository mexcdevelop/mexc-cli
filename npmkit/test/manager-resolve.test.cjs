'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const manager = require('../runtime/lib/manager.cjs');
const semver = require('../runtime/lib/semver.cjs');

function tempDir(t, prefix) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  t.after(() => {
    // Leave the directory before removing it. One test below chdirs INTO one of
    // these, and Windows refuses to remove the process's own current directory
    // (EPERM) — a teardown failure that reads exactly like a broken resolver.
    //
    // Done in the fixture rather than by ordering the hooks: t.after runs in
    // registration order, so this cleanup fires BEFORE the test's own chdir-back,
    // and a fixture should not depend on which hook was registered first.
    const cwd = process.cwd();
    if (cwd === dir || cwd.toLowerCase().startsWith(dir.toLowerCase() + path.sep)) {
      process.chdir(os.tmpdir());
    }
    fs.rmSync(dir, { recursive: true, force: true });
  });
  return dir;
}

// Windows filesystems are case-insensitive and PATHEXT is conventionally
// upper-case, so the resolver legitimately returns 'npm.CMD' for a file written as
// 'npm.cmd'. Comparing exactly would be asserting a property the modelled platform
// does not have.
function samePath(left, right) {
  return String(left).toLowerCase() === String(right).toLowerCase();
}

function shim(dir, name) {
  const file = path.join(dir, name);
  fs.writeFileSync(file, '@echo off\n', { mode: 0o755 });
  return file;
}

// THE security property. cmd.exe resolves a bare command name against the CURRENT
// DIRECTORY before PATH, and this code runs the package manager from the update
// notifier — detached, output discarded, on ordinary commands. A planted npm.cmd
// in whatever directory the user happened to be in would run silently with their
// token. Resolution therefore has to happen here, against PATH only.
//
// The process actually chdir's into the directory holding the plant, because that
// is the only way '' and '.' mean what they mean on Windows. An earlier draft put
// the plant in a directory that was ALSO a real PATH entry, which made resolving
// to it correct and the test meaningless.
//
// Driven with platform='win32' from any host: the lookup is pure and takes the
// platform, which is the only way this branch is reachable off Windows.
test('the package manager is resolved from PATH, never from the current directory', (t) => {
  const onPath = tempDir(t, 'mexc-path-');
  const cwdLike = tempDir(t, 'mexc-cwd-');
  shim(cwdLike, 'npm.cmd');
  const real = shim(onPath, 'npm.cmd');

  const previous = process.cwd();
  process.chdir(cwdLike);
  t.after(() => process.chdir(previous));

  // '' and '.' are the two shapes Windows reads as "the current directory", and
  // both are listed BEFORE the only legitimate entry.
  const env = {
    PATHEXT: '.COM;.EXE;.BAT;.CMD',
    PATH: ['', '.', onPath].join(path.delimiter),
  };

  const resolved = manager.resolveOnPath('npm', env, 'win32');
  assert.ok(resolved, 'expected the real PATH entry to resolve');
  assert.ok(path.isAbsolute(resolved), `must be absolute so the shell has nothing to search, got ${resolved}`);
  assert.ok(samePath(resolved, real), `expected ${real}, got ${resolved}`);
  // Belt and braces: whatever it returned must not live under the current directory.
  assert.ok(!samePath(path.dirname(resolved), cwdLike), 'resolved into the current directory');
});

// With '' and '.' the ONLY entries, there is no legitimate directory to find the
// manager in, and the answer must be "not found" rather than the planted file.
test('an all-current-directory PATH resolves to nothing rather than to the plant', (t) => {
  const cwdLike = tempDir(t, 'mexc-cwd-');
  shim(cwdLike, 'npm.cmd');
  const env = { PATHEXT: '.CMD', PATH: ['', '.'].join(path.delimiter) };

  assert.equal(manager.resolveOnPath('npm', env, 'win32'), null);
});

test('PATHEXT order decides, and a missing manager is null rather than a bare name', (t) => {
  const dir = tempDir(t, 'mexc-ext-');
  shim(dir, 'npm.cmd');
  shim(dir, 'npm.exe');
  const env = { PATHEXT: '.EXE;.CMD', PATH: dir };

  assert.ok(samePath(manager.resolveOnPath('npm', env, 'win32'), path.join(dir, 'npm.exe')));
  assert.equal(manager.resolveOnPath('pnpm', env, 'win32'), null,
    'a manager that is not installed must not fall through to a bare name the shell would search');
});

// Unix does not need any of this: there is no .cmd problem, so no shell, so no
// resolution to get wrong. The command is passed through untouched.
test('non-Windows passes the command through unchanged', () => {
  assert.equal(manager.resolveOnPath('npm', { PATH: '/nowhere' }, 'linux'), 'npm');
  assert.equal(manager.resolveOnPath('pnpm', {}, 'darwin'), 'pnpm');
});

// The version that reaches the install command must be the one the validator
// blessed. semver.parse validates value.trim(); parseViewVersion returns the JSON
// string untrimmed. Validating one and installing the other is how a value that
// "passed validation" still reaches a command line with whitespace in it.
test('a registry version is normalised before it is used, not merely validated', () => {
  const announced = manager.parseViewVersion(JSON.stringify(' 1.2.3\n'));
  assert.equal(announced, ' 1.2.3\n', 'parseViewVersion is expected to return the raw JSON string');

  const parsed = semver.parse(announced);
  assert.ok(parsed, 'the untrimmed form still passes validation — that is the trap');
  assert.equal(parsed.raw, '1.2.3', 'and .raw is the normalised value callers must use');
  assert.notEqual(parsed.raw, announced);
});

// An explicit --registry outranks .npmrc, npm_config_registry and any
// @scope:registry override, so passing an address unconditionally is exactly how
// `update` came to ignore the registry the user installed from. The first attempt
// must therefore carry no --registry at all, and the fallback must be the PUBLIC
// registry — never the one the package happened to be published to, which is not
// a fact about the package and does not ship any more.
test('a version query follows the user configuration first and falls back to the public registry', () => {
  const calls = [];
  const runner = (_command, args) => {
    calls.push(args);
    return args.includes('--registry')
      ? { status: 0, stdout: '"1.4.0"\n' }
      : { status: 1, stderr: 'npm error code E404' };
  };
  const found = manager.queryVersion('npm', '@example/cli', 'latest', runner);

  assert.equal(found.version, '1.4.0');
  assert.equal(found.registry, manager.PUBLIC_REGISTRY,
    'the caller must install from whichever candidate answered, not from the first one tried');
  assert.equal(calls.length, 2);
  assert.ok(!calls[0].includes('--registry'), 'the first attempt must let npm resolve the registry itself');
  assert.deepEqual(calls[1].slice(-2), ['--registry', manager.PUBLIC_REGISTRY]);
});
