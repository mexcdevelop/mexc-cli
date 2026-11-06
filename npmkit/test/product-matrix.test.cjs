'use strict';

// The platform matrix, asserted from the outside.
//
// loadProduct's gate only checks that a product's platforms are a subset of what
// the kit can build (see the comment there for why it is a subset). A subset
// check cannot notice a product that silently LOSES a platform: the packaging
// run stays green and the product simply stops shipping it, which is invisible
// until someone reports that `npm i` picks nothing on their machine.
//
// So the "did we mean to ship exactly these?" question lives here instead, as a
// table this file owns. Changing a product's matrix means changing EXPECTED in
// the same commit — that is the point, not an inconvenience.
//
// The table is written out rather than derived from the product files, because
// deriving the expectation from the thing under test would assert nothing.

const assert = require('node:assert/strict');
const childProcess = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const kitRoot = path.resolve(__dirname, '..');
const repo = path.resolve(kitRoot, '..');
const platformsFile = path.join(kitRoot, 'platforms.json');
const supported = JSON.parse(fs.readFileSync(platformsFile, 'utf8'));

const PLATFORM_FIELDS = ['id', 'nodePlatform', 'nodeArch', 'goos', 'goarch'];

// Every product this repository packages, and the platform ids it must declare.
//
// This list is the DECISION to publish, kept apart from platforms.json which is
// only the kit's CAPABILITY. The kit knowing how to build a target is not the
// same as this repository choosing to upload a package for it, and the gate in
// package.mjs is a subset check precisely so the two can differ.
//
// Adding a platform here is not what exposes it to users, and removing one is
// not what holds it back: that is the release. Packages reach the registry only
// from a formal vMAJOR.MINOR.PATCH tag, through .github/workflows/release.yml, so
// a target can be declared here and still be kept out of every install by not
// tagging it into a release.
//
// Every shipped target has been exercised as a real binary on its own platform:
// scripts/verify-windows.ps1 on a Windows machine, scripts/verify-linux.sh in
// containers, and macOS natively. Keep it that way before tagging a release.
const SHIPPED = [
  'darwin-arm64', 'darwin-x64',
  'linux-arm64', 'linux-x64',
  'win32-arm64', 'win32-x64',
];

const EXPECTED = {
  'npm/product.json': SHIPPED,
  'npm/product-unscoped.json': SHIPPED,
};

function readProduct(relative) {
  return JSON.parse(fs.readFileSync(path.join(repo, relative), 'utf8'));
}

function runKit(args, cwd) {
  return childProcess.spawnSync(process.execPath, [path.join(kitRoot, 'package.mjs'), ...args], {
    cwd,
    encoding: 'utf8',
  });
}

function git(directory, ...args) {
  childProcess.execFileSync('git', args, { cwd: directory, stdio: 'ignore' });
}

test('the kit platform table is complete and free of duplicate ids', () => {
  assert.ok(Array.isArray(supported) && supported.length > 0, 'platforms.json must be a non-empty array');
  const ids = new Set();
  for (const platform of supported) {
    for (const field of PLATFORM_FIELDS) {
      assert.equal(typeof platform[field], 'string', `platforms.json entry is missing ${field}: ${JSON.stringify(platform)}`);
      assert.ok(platform[field].length > 0, `platforms.json entry has an empty ${field}: ${JSON.stringify(platform)}`);
    }
    assert.equal(platform.id, `${platform.nodePlatform}-${platform.nodeArch}`,
      `platforms.json id must be "<nodePlatform>-<nodeArch>": ${JSON.stringify(platform)}`);
    assert.equal(ids.has(platform.id), false, `platforms.json declares ${platform.id} twice`);
    ids.add(platform.id);
  }
});

test('every product declares exactly the platforms it is meant to ship', () => {
  for (const [relative, expected] of Object.entries(EXPECTED)) {
    const product = readProduct(relative);
    const declared = product.platforms.map((platform) => platform.id).sort();
    assert.deepEqual(declared, [...expected].sort(),
      `${relative} platform ids drifted from EXPECTED in ${path.basename(__filename)}`);
  }
});

test('every product platform agrees field by field with the kit table', () => {
  const byID = new Map(supported.map((platform) => [platform.id, platform]));
  for (const relative of Object.keys(EXPECTED)) {
    for (const platform of readProduct(relative).platforms) {
      const kit = byID.get(platform.id);
      assert.ok(kit, `${relative} declares ${platform.id}, which platforms.json does not list`);
      for (const field of PLATFORM_FIELDS) {
        assert.equal(platform[field], kit[field],
          `${relative} sets ${platform.id}.${field}=${platform[field]}, kit says ${kit[field]}`);
      }
    }
  }
});

// The three tests below drive package.mjs itself, so they assert the gate's
// behaviour rather than restating its source. loadProduct runs before any git
// or filesystem work, so the two rejection cases need no repository fixture.

test('the gate rejects a platform the kit cannot build', (t) => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-matrix-'));
  t.after(() => fs.rmSync(temp, { recursive: true, force: true }));
  const product = readProduct('npm/product.json');
  product.platforms = [{
    id: 'plan9-mips', nodePlatform: 'plan9', nodeArch: 'mips', goos: 'plan9', goarch: 'mips',
  }];
  const file = path.join(temp, 'npm', 'product.json');
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, `${JSON.stringify(product, null, 2)}\n`);

  const result = runKit([
    '--product', file,
    '--skip-build',
    '--binaries-dir', temp,
  ], temp);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /plan9-mips is not one this kit can build/);
});

test('the gate rejects a platform whose build target disagrees with the kit', (t) => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-matrix-'));
  t.after(() => fs.rmSync(temp, { recursive: true, force: true }));
  const product = readProduct('npm/product.json');
  // Same id the kit knows, but pointed at the wrong Go target — the mistake a
  // whole-object compare caught and a bare id lookup would not.
  product.platforms = [{ ...supported[0], goarch: 'wrong' }];
  const file = path.join(temp, 'npm', 'product.json');
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, `${JSON.stringify(product, null, 2)}\n`);

  const result = runKit([
    '--product', file,
    '--skip-build',
    '--binaries-dir', temp,
  ], temp);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /sets goarch="wrong" but this kit builds it as/);
});

// The reason the gate was loosened: a product must be able to ship FEWER
// platforms than the kit can build, so the kit's matrix and a consuming
// repository's product no longer have to move in lockstep.
test('the gate accepts a product shipping a strict subset of the kit matrix', (t) => {
  if (supported.length < 2) {
    t.skip('needs at least two supported platforms for a subset to be strict');
    return;
  }
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-matrix-'));
  t.after(() => fs.rmSync(temp, { recursive: true, force: true }));

  // The single platform has to be one THIS HOST can package, or the run is
  // refused before the gate under test is reached: npm packaging cannot give a
  // POSIX binary its executable bit on Windows (assertHostCanPack in
  // package.mjs), and a windows-only matrix is precisely the case it lets
  // through. Picking by host keeps this test running everywhere instead of
  // skipping on one of the platforms the products ship — the subject is the
  // subset gate, which has nothing to do with the host.
  const packable = process.platform === 'win32'
    ? supported.filter((platform) => platform.goos === 'windows')
    : supported;
  if (packable.length === 0) {
    t.skip('no platform in the kit matrix can be packaged on this host');
    return;
  }
  const only = packable[0];
  const product = readProduct('npm/product.json');
  product.platforms = [only];
  const file = path.join(temp, 'npm', 'product.json');
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, `${JSON.stringify(product, null, 2)}\n`);

  git(temp, 'init');
  git(temp, 'config', 'user.name', 'Matrix Test');
  git(temp, 'config', 'user.email', 'matrix-test@example.invalid');
  git(temp, 'add', 'npm/product.json');
  git(temp, 'commit', '-m', 'test: initialize fixture');

  // Only the one declared platform gets a binary; that is what a subset means.
  const binaries = path.join(temp, 'input');
  fs.mkdirSync(path.join(binaries, only.id), { recursive: true });
  // The staged binary must carry the name the packager looks for, and that
  // differs by target: Windows cannot execute a file without the extension.
  const fixtureBinary = only.goos === 'windows' ? `${product.binaryName}.exe` : product.binaryName;
  fs.writeFileSync(path.join(binaries, only.id, fixtureBinary), `fixture-${only.id}\n`, { mode: 0o755 });

  const output = path.join(temp, 'output');
  const result = runKit([
    '--product', file,
    '--output', output,
    '--snapshot',
    '--version', '1.2.3-beta.1',
    '--skip-build',
    '--binaries-dir', binaries,
  ], temp);
  assert.equal(result.status, 0, result.stderr);

  const manifest = JSON.parse(fs.readFileSync(path.join(output, 'release-manifest.json'), 'utf8'));
  assert.deepEqual(manifest.platforms.map((platform) => platform.id), [only.id]);

  const main = JSON.parse(fs.readFileSync(path.join(output, 'stage', 'main', 'package.json'), 'utf8'));
  assert.deepEqual(Object.keys(main.optionalDependencies), [`${product.packageName}-${only.id}`]);
});
