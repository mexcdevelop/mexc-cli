'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const semver = require('../runtime/lib/semver.cjs');
const platform = require('../runtime/lib/platform.cjs');

const supported = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'platforms.json'), 'utf8'));

test('Semantic Version comparison and release channel follow npm precedence', () => {
  assert.equal(semver.compare('1.2.3', '1.2.3-beta.9'), 1);
  assert.equal(semver.compare('1.2.3-beta.10', '1.2.3-beta.2'), 1);
  assert.equal(semver.compare('1.2.3+build.4', '1.2.3+build.1'), 0);
  assert.equal(semver.channel('2.0.0'), 'latest');
  assert.equal(semver.channel('2.0.0-rc.1'), 'beta');
  assert.equal(semver.parse('v1.2.3'), null);
  assert.equal(semver.parse('1.02.3'), null);
});

// Driven by platforms.json rather than a written-out list. The earlier version
// asserted that linux/x64 is refused, which was true of its synthetic config but
// read as a claim about what the product ships — so it would have kept passing
// while quietly contradicting a shipped platform.
test('the platform table maps every shipped target and refuses the rest', () => {
  const config = { packageName: '@example/cli', platforms: {} };
  for (const { id } of supported) {
    config.platforms[id] = { packageName: `@example/cli-${id}`, binary: 'bin/example' };
  }
  for (const { nodePlatform, nodeArch, id } of supported) {
    assert.equal(platform.selected(config, nodePlatform, nodeArch).packageName, `@example/cli-${id}`);
  }
  // A target the kit has no recipe for, so it stays unsupported however the
  // matrix grows.
  assert.throws(() => platform.selected(config, 'plan9', 'mips'), /unsupported platform plan9\/mips/);
  // The refusal names what IS installable, so a user on an unsupported machine
  // learns the answer from the failure. That list is on the error's hint, not
  // its message, which is why this reads the field instead of matching prose.
  try {
    platform.selected(config, 'plan9', 'mips');
    assert.fail('an unsupported platform must be refused');
  } catch (error) {
    assert.equal(error.hint, `install @example/cli on one of: ${supported.map(({ id }) => id).sort().join(', ')}`);
  }
});

test('binary resolution requires an installed platform package at the exact main version', (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-platform-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.writeFileSync(path.join(root, 'package.json'), '{"name":"@example/cli"}\n');
  const packageRoot = path.join(root, 'node_modules', '@example', 'cli-darwin-arm64');
  fs.mkdirSync(path.join(packageRoot, 'bin'), { recursive: true });
  fs.writeFileSync(path.join(packageRoot, 'package.json'), '{"name":"@example/cli-darwin-arm64","version":"1.2.3"}\n');
  fs.writeFileSync(path.join(packageRoot, 'bin', 'example'), 'binary');
  const config = {
    packageName: '@example/cli',
    version: '1.2.3',
    platforms: { 'darwin-arm64': { packageName: '@example/cli-darwin-arm64', binary: 'bin/example' } },
  };
  assert.equal(
    fs.realpathSync(platform.resolveBinary(config, root, 'darwin', 'arm64').binary),
    fs.realpathSync(path.join(packageRoot, 'bin', 'example')),
  );
  fs.writeFileSync(path.join(packageRoot, 'package.json'), '{"name":"@example/cli-darwin-arm64","version":"1.2.4"}\n');
  assert.throws(() => platform.resolveBinary(config, root, 'darwin', 'arm64'), /version mismatch/);
  fs.rmSync(packageRoot, { recursive: true, force: true });
  assert.throws(() => platform.resolveBinary(config, root, 'darwin', 'arm64'), /is not installed/);
});
