'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const manager = require('../runtime/lib/manager.cjs');
const cache = require('../runtime/lib/cache.cjs');

const config = {
  packageName: '@example/cli',
  commandName: 'example',
  stateDir: '.example',
  registry: 'https://registry.example.invalid/',
  version: '1.2.3',
  refreshMs: 60 * 60_000,
};

test('package manager helpers parse registry output and preserve exact versions', () => {
  assert.equal(manager.parseViewVersion('"1.2.3"\n'), '1.2.3');
  assert.equal(manager.parseViewVersion('["1.2.2","1.2.3"]\n'), '1.2.3');
  assert.equal(manager.parseViewVersion("'1.2.3'\n"), '1.2.3');
  assert.equal(manager.managerFromUserAgent({ npm_config_user_agent: 'pnpm/9.1.0 npm/? node/v20' }), 'pnpm');
  assert.equal(
    manager.exactInstallCommand('npm', '@example/cli', '1.2.3', config.registry, true),
    'npm install --save-exact @example/cli@1.2.3 --registry https://registry.example.invalid/',
  );
  assert.equal(
    manager.exactNPXCommand('@example/cli', '1.2.3', 'example', config.registry),
    'npx --yes --registry https://registry.example.invalid/ --package @example/cli@1.2.3 -- example --version',
  );
});

test('global installation detection requires the current package real path', (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-global-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const mainRoot = manager.packagePath(root, config.packageName);
  fs.mkdirSync(mainRoot, { recursive: true });
  const runner = (command, args) => command === 'npm' && args.join(' ') === 'root --global'
    ? { status: 0, stdout: `${root}\n` }
    : { status: 1, stderr: 'not configured' };
  assert.deepEqual(manager.detectInstallation(mainRoot, config.packageName, {}, runner), {
    mode: 'global', manager: 'npm', globalRoot: root,
  });
  assert.equal(manager.detectInstallation(path.join(root, 'other'), config.packageName, {}, runner).mode, 'unknown');
  const pnpmRunner = (command, args) => command === 'pnpm' && args.join(' ') === 'root --global'
    ? { status: 0, stdout: `${root}\n` }
    : { status: 1, stderr: 'not configured' };
  assert.equal(manager.detectInstallation(mainRoot, config.packageName, {}, pnpmRunner).manager, 'pnpm');
});

test('notifier cache is product-scoped, atomic, permissioned, and leased', (t) => {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-cache-'));
  t.after(() => fs.rmSync(home, { recursive: true, force: true }));
  const checkedAt = new Date('2026-08-10T00:00:00Z');
  const value = cache.write(config, 'latest', '1.3.0', checkedAt, home);
  assert.equal(value.current, '1.2.3');
  assert.equal(value.latest, '1.3.0');
  assert.deepEqual(cache.read(config, 'latest', home), value);
  assert.equal(cache.read({ ...config, packageName: '@other/cli' }, 'latest', home), null);
  // Only where the bits are real. Windows derives them from a single attribute
  // (FILE_ATTRIBUTE_READONLY), so every writable file reports 0666 and this
  // asserts a mode no file there can hold — the same reason the Go side stopped
  // consulting them (see secfile.ModeBitsMeaningful). What actually protects the
  // file there is the ACL on the per-user directory, which this kit does not set
  // and does not inspect; the rest of this test is platform-independent, so the
  // one line is guarded rather than the whole test skipped.
  if (process.platform !== 'win32') {
    assert.equal(fs.statSync(cache.paths(config, home).cache).mode & 0o777, 0o600);
  }
  assert.equal(cache.fresh(value, config, checkedAt.getTime() + config.refreshMs - 1), true);
  assert.equal(cache.fresh(value, config, checkedAt.getTime() + config.refreshMs), false);
  const lease = cache.acquireLease(config, home, checkedAt.getTime());
  assert.ok(lease);
  assert.equal(cache.acquireLease(config, home, checkedAt.getTime()), null);
  cache.releaseLease(config, home);
  assert.ok(cache.acquireLease(config, home, checkedAt.getTime()));
  fs.writeFileSync(cache.paths(config, home).cache, '{broken json\n');
  assert.equal(cache.read(config, 'latest', home), null);
});

test('notifier eligibility excludes noninteractive and control commands', () => {
  const tty = { isTTY: true };
  assert.equal(cache.eligible(config, ['spot'], {}, tty), true);
  assert.equal(cache.eligible(config, ['update'], {}, tty), false);
  assert.equal(cache.eligible(config, ['completion'], {}, tty), false);
  assert.equal(cache.eligible(config, ['mcp'], {}, tty), false);
  assert.equal(cache.eligible(config, ['spot'], { CI: '1' }, tty), false);
  assert.equal(cache.eligible(config, ['spot'], { MEXC_CLI_NO_UPDATE_NOTIFIER: '1' }, tty), false);
  assert.equal(cache.eligible(config, ['spot'], { MEXC_CLI_NO_UPDATE_NOTIFIER: '0' }, tty), true);
  assert.equal(cache.eligible(config, ['spot'], {}, { isTTY: false }), false);
  assert.equal(cache.eligible({ ...config, version: 'dev' }, ['spot'], {}, tty), false);
});

test('stale notifier cache starts one detached refresh without touching stdout', (t) => {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-notify-'));
  t.after(() => fs.rmSync(home, { recursive: true, force: true }));
  const stderr = { isTTY: true, text: '', write(value) { this.text += value; } };
  let calls = 0;
  const spawn = (_command, _args, options) => {
    calls += 1;
    assert.equal(options.detached, true);
    assert.equal(options.stdio, 'ignore');
    return { unref() {} };
  };
  cache.maybeNotify(config, ['spot'], { home, stderr, env: {}, spawn, now: Date.now() });
  cache.maybeNotify(config, ['spot'], { home, stderr, env: {}, spawn, now: Date.now() });
  assert.equal(calls, 1);
  assert.equal(stderr.text, '');
});
