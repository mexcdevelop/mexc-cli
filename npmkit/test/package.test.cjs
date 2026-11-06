'use strict';

const assert = require('node:assert/strict');
const childProcess = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const hosts = require('./hosts.cjs');

const kitRoot = path.resolve(__dirname, '..');
const repo = path.resolve(kitRoot, '..');
const productFile = path.join(repo, 'npm', 'product.json');
const unscopedFile = path.join(repo, 'npm', 'product-unscoped.json');

// Two tables, and conflating them is a real mistake this file made: kitPlatforms
// is what the kit CAN build, product.platforms is what one product SHIPS. They
// were the same list until Windows became buildable but not yet publishable, at
// which point every count derived from the kit table over-counted by two.
//
// Anything asserting about a packaging RUN must come from the product being
// packaged. kitPlatforms is only for fixtures, which may cover more.
const kitPlatforms = JSON.parse(fs.readFileSync(path.join(kitRoot, 'platforms.json'), 'utf8'));

function productPlatforms(file) {
  return JSON.parse(fs.readFileSync(file, 'utf8')).platforms;
}

function chmodTree(directory, directoryMode, fileMode) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) chmodTree(full, directoryMode, fileMode);
    else fs.chmodSync(full, fileMode);
  }
  fs.chmodSync(directory, directoryMode);
}

// The file name has to match what the kit looks for, which is per-platform:
// Windows will not run a file with no executable extension, so the kit writes and
// ships mexc.exe there. Hardcoding 'mexc' made these fixtures silently
// platform-specific — the packaging run failed on a missing file rather than on
// anything the test was about.
function fixtureBinaryName(platform) {
  return platform.goos === 'windows' ? 'mexc.exe' : 'mexc';
}

function writeFixtureBinaries(directory, list = kitPlatforms) {
  for (const platform of list) {
    const target = path.join(directory, platform.id);
    fs.mkdirSync(target, { recursive: true });
    fs.writeFileSync(path.join(target, fixtureBinaryName(platform)), `fixture-${platform.id}\n`, { mode: 0o755 });
  }
}

function git(directory, ...args) {
  childProcess.execFileSync('git', args, { cwd: directory, stdio: 'ignore' });
}

test('packaging produces allowlisted exact-version tarballs and manifest evidence', (t) => {
  if (hosts.requirePosixPackagingHost(t)) return;
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-pack-'));
  t.after(() => {
    const copiedRuntime = path.join(temp, 'kit', 'runtime');
    if (fs.existsSync(copiedRuntime)) chmodTree(copiedRuntime, 0o755, 0o644);
    fs.rmSync(temp, { recursive: true, force: true });
  });
  const copiedKit = path.join(temp, 'kit');
  fs.cpSync(kitRoot, copiedKit, { recursive: true });
  chmodTree(path.join(copiedKit, 'runtime'), 0o555, 0o444);
  const binaries = path.join(temp, 'input');
  writeFixtureBinaries(binaries);
  const output = path.join(temp, 'output');
  const result = childProcess.spawnSync(process.execPath, [path.join(copiedKit, 'package.mjs'),
    '--product', productFile,
    '--output', output,
    '--snapshot',
    '--version', '1.2.3-beta.1',
    '--skip-build',
    '--binaries-dir', binaries,
  ], { cwd: repo, encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  const manifest = JSON.parse(fs.readFileSync(path.join(output, 'release-manifest.json'), 'utf8'));
  assert.equal(manifest.mode, 'snapshot');
  assert.equal(manifest.version, '1.2.3-beta.1');
  assert.equal(manifest.tag, null);
  // Counts are derived from the platform table, not written out: they are a
  // restatement of its size, and hardcoding them means every matrix change
  // breaks tests that have no opinion about which platforms ship. The "did we
  // mean to ship exactly these?" question lives in product-matrix.test.cjs.
  const shipped = productPlatforms(productFile);
  assert.equal(manifest.packages.length, shipped.length + 1); // one per platform, plus main
  assert.equal(manifest.binaries.length, shipped.length);
  assert.equal(manifest.verification.status, 'passed');
  assert.ok(manifest.packages.every((item) => item.version === manifest.version && item.integrity.startsWith('sha512-')));
  const main = JSON.parse(fs.readFileSync(path.join(output, 'stage', 'main', 'package.json'), 'utf8'));
  assert.deepEqual(Object.values(main.optionalDependencies), Array(shipped.length).fill(manifest.version));
  assert.equal(main.scripts, undefined);
  assert.equal(fs.existsSync(path.join(output, 'stage', 'main', '.npmrc')), false);
  const distribution = JSON.parse(fs.readFileSync(path.join(output, 'stage', 'main', 'lib', 'distribution.json'), 'utf8'));
  // No registry anywhere: not in the metadata that ships, and not in the build
  // evidence either. Where a package was published is not a fact about the
  // package, the tarballs are identical whoever hosts them, and naming an address
  // put an internal hostname into a published tree.
  assert.equal(distribution.registry, undefined);
  assert.equal(manifest.registry, undefined);
  for (const platform of shipped) {
    const packageJSON = JSON.parse(fs.readFileSync(path.join(output, 'stage', platform.id, 'package.json'), 'utf8'));
    assert.deepEqual(packageJSON.os, [platform.nodePlatform]);
    assert.deepEqual(packageJSON.cpu, [platform.nodeArch]);
    assert.equal(packageJSON.version, manifest.version);

    // The executable extension has to agree in all THREE places, or the launcher
    // resolves a path that is not there: the file staged into bin/, the files[]
    // allowlist that decides what npm actually packs, and the per-platform binary
    // path the runtime reads out of distribution.json. Two out of three ships a
    // package that installs cleanly and cannot run.
    const expected = platform.goos === 'windows' ? 'mexc.exe' : 'mexc';
    assert.ok(
      fs.existsSync(path.join(output, 'stage', platform.id, 'bin', expected)),
      `${platform.id}: staged binary is not named ${expected}`,
    );
    assert.deepEqual(packageJSON.files, [`bin/${expected}`], `${platform.id}: files[] does not list ${expected}`);
    assert.equal(
      distribution.platforms[platform.id].binary,
      `bin/${expected}`,
      `${platform.id}: distribution.json points the launcher at the wrong file`,
    );
  }
});

test('packaging builds the official unscoped product with unscoped platform packages', (t) => {
  if (hosts.requirePosixPackagingHost(t)) return;
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-unscoped-pack-'));
  t.after(() => fs.rmSync(temp, { recursive: true, force: true }));
  const binaries = path.join(temp, 'input');
  writeFixtureBinaries(binaries);
  const output = path.join(temp, 'output');
  const result = childProcess.spawnSync(process.execPath, [path.join(kitRoot, 'package.mjs'),
    '--product', unscopedFile,
    '--output', output,
    '--snapshot',
    '--version', '1.2.3-beta.1',
    '--skip-build',
    '--binaries-dir', binaries,
  ], { cwd: repo, encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  const manifest = JSON.parse(fs.readFileSync(path.join(output, 'release-manifest.json'), 'utf8'));
  const main = JSON.parse(fs.readFileSync(path.join(output, 'stage', 'main', 'package.json'), 'utf8'));
  assert.equal(manifest.product, 'mexc-ai-cli');
  assert.equal(main.name, 'mexc-ai-cli');
  assert.deepEqual(
    Object.keys(main.optionalDependencies).sort(),
    productPlatforms(unscopedFile).map((platform) => `mexc-ai-cli-${platform.id}`).sort(),
  );
});

test('snapshot packaging requires an explicit prerelease version', () => {
  const base = [path.join(kitRoot, 'package.mjs'),
    '--product', productFile,
    '--snapshot',
  ];
  const missing = childProcess.spawnSync(process.execPath, base, { cwd: repo, encoding: 'utf8' });
  assert.notEqual(missing.status, 0);
  assert.match(missing.stderr, /--snapshot requires --version/);

  const stable = childProcess.spawnSync(process.execPath, [
    ...base,
    '--version', '1.2.3',
  ], { cwd: repo, encoding: 'utf8' });
  assert.notEqual(stable.status, 0);
  assert.match(stable.stderr, /must include a prerelease identifier/);
});

test('release packaging does not accept an explicit version', () => {
  const result = childProcess.spawnSync(process.execPath, [path.join(kitRoot, 'package.mjs'),
    '--product', productFile,
    '--version', '1.2.3',
  ], { cwd: repo, encoding: 'utf8' });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /--version requires --snapshot/);
});

test('release packaging uses an exact HEAD tag when generated files are present', (t) => {
  if (hosts.requirePosixPackagingHost(t)) return;
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-release-'));
  t.after(() => fs.rmSync(temp, { recursive: true, force: true }));
  const isolatedProduct = path.join(temp, 'npm', 'product.json');
  fs.mkdirSync(path.dirname(isolatedProduct), { recursive: true });
  fs.copyFileSync(productFile, isolatedProduct);
  git(temp, 'init');
  git(temp, 'config', 'user.name', 'Packaging Test');
  git(temp, 'config', 'user.email', 'packaging-test@example.invalid');
  git(temp, 'add', 'npm/product.json');
  git(temp, 'commit', '-m', 'test: initialize fixture');

  const binaries = path.join(temp, 'input');
  writeFixtureBinaries(binaries);
  assert.equal(fs.existsSync(path.join(binaries, kitPlatforms[0].id, 'mexc')), true);
  const output = path.join(temp, 'output');
  const args = [path.join(kitRoot, 'package.mjs'),
    '--product', isolatedProduct,
    '--output', output,
    '--skip-build',
    '--binaries-dir', binaries,
  ];
  const untagged = childProcess.spawnSync(process.execPath, args, { cwd: temp, encoding: 'utf8' });
  assert.notEqual(untagged.status, 0);
  assert.match(untagged.stderr, /does not have exactly one valid release tag/);

  git(temp, 'tag', 'v1.2.3');
  const tagged = childProcess.spawnSync(process.execPath, args, { cwd: temp, encoding: 'utf8' });
  assert.equal(tagged.status, 0, tagged.stderr);
  const manifest = JSON.parse(fs.readFileSync(path.join(output, 'release-manifest.json'), 'utf8'));
  assert.equal(manifest.mode, 'release');
  assert.equal(manifest.version, '1.2.3');
  assert.equal(manifest.tag, 'v1.2.3');
});

// The packaging modes take no registry at all any more, so an unknown flag is the
// only answer left for one. Pinned here because the flag existed for a long time
// and a caller that still passes it must be told, not quietly obeyed.
test('packaging refuses a registry argument', () => {
  const result = childProcess.spawnSync(process.execPath, [path.join(kitRoot, 'package.mjs'),
    '--product', productFile,
    '--registry', 'https://registry.example.invalid/npm',
    '--skip-build',
    '--binaries-dir', os.tmpdir(),
  ], { cwd: repo, encoding: 'utf8' });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /unknown argument --registry/);
});

// writeProductVariant copies the real product descriptor with some fields
// changed, so a test can probe one rule without restating the whole descriptor.
// The kit takes the repository to be the descriptor's grandparent, so the copy
// goes into <directory>/npm/ and <directory> becomes a one-commit repository
// with no LICENSE in it.
function writeProductVariant(directory, changes) {
  const product = { ...JSON.parse(fs.readFileSync(productFile, 'utf8')), ...changes };
  Object.keys(changes)
    .filter((key) => changes[key] === undefined)
    .forEach((key) => delete product[key]);
  fs.mkdirSync(path.join(directory, 'npm'));
  const file = path.join(directory, 'npm', 'product.json');
  fs.writeFileSync(file, `${JSON.stringify(product, null, 2)}\n`);
  git(directory, 'init', '-q');
  git(directory, '-c', 'user.name=test', '-c', 'user.email=test@example.com', 'commit', '-q', '--allow-empty', '-m', 'test: initialize fixture');
  return file;
}

function packFixture(t, product) {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-license-pack-'));
  t.after(() => fs.rmSync(temp, { recursive: true, force: true }));
  const binaries = path.join(temp, 'input');
  writeFixtureBinaries(binaries);
  const output = path.join(temp, 'output');
  const result = childProcess.spawnSync(process.execPath, [path.join(kitRoot, 'package.mjs'),
    '--product', product(temp),
    '--output', output,
    '--snapshot',
    '--version', '1.2.3-beta.1',
    '--skip-build',
    '--binaries-dir', binaries,
  ], { cwd: repo, encoding: 'utf8' });
  return { result, output };
}

// The published product is MIT and says where its source lives: npm trusted
// publishing compares `repository` with the repository the publishing workflow
// runs in, and refuses a package that names another one or none.
test('the published product ships MIT, its LICENSE and its repository in every package', (t) => {
  if (hosts.requirePosixPackagingHost(t)) return;
  const { result, output } = packFixture(t, () => productFile);
  assert.equal(result.status, 0, result.stderr);
  const product = JSON.parse(fs.readFileSync(productFile, 'utf8'));
  for (const id of ['main', ...productPlatforms(productFile).map((platform) => platform.id)]) {
    const packageJSON = JSON.parse(fs.readFileSync(path.join(output, 'stage', id, 'package.json'), 'utf8'));
    assert.equal(packageJSON.license, 'MIT', `${id}: license`);
    assert.deepEqual(packageJSON.repository, { type: 'git', url: product.repository }, `${id}: repository`);
    assert.ok(fs.existsSync(path.join(output, 'stage', id, 'LICENSE')), `${id}: LICENSE was not staged`);
  }
});

// UNLICENSED stays legal: a downstream repository packages a proprietary
// product with a synced copy of this kit, and declares no repository.
test('a proprietary product without a repository still packages, unchanged', (t) => {
  if (hosts.requirePosixPackagingHost(t)) return;
  const { result, output } = packFixture(t, (temp) => writeProductVariant(temp, { license: 'UNLICENSED', repository: undefined }));
  assert.equal(result.status, 0, result.stderr);
  const main = JSON.parse(fs.readFileSync(path.join(output, 'stage', 'main', 'package.json'), 'utf8'));
  assert.equal(main.license, 'UNLICENSED');
  assert.equal(main.repository, undefined);
});

test('packaging refuses a license outside the allowlist and a malformed repository', (t) => {
  const badLicense = packFixture(t, (temp) => writeProductVariant(temp, { license: 'GPL-3.0' })).result;
  assert.notEqual(badLicense.status, 0);
  assert.match(badLicense.stderr, /license must be one of MIT, UNLICENSED/);
  const badRepository = packFixture(t, (temp) => writeProductVariant(temp, { repository: 'https://github.com/mexcdevelop/mexc-cli' })).result;
  assert.notEqual(badRepository.status, 0);
  assert.match(badRepository.stderr, /repository must look like git\+https:/);
});

// The Windows naming, asserted against a product that declares ONLY Windows.
//
// The products now ship win32, so the assertions above cover it too — but they
// cover it mixed in with three other platforms, where a bug that applies the
// suffix to every target, or to none, can hide behind a passing majority. This
// isolates the one platform whose name differs.
//
// It also keeps the branch under test if the products ever stop declaring
// win32 again: the fixture supplies its own product descriptor, so this does not
// depend on what npm/product.json happens to say today.
//
// All three places have to agree or the launcher resolves a path that is not
// there: the file staged into bin/, the files[] allowlist that decides what npm
// actually packs, and the per-platform binary path the runtime reads out of
// distribution.json. Two out of three ships a package that installs and cannot run.
test('a product declaring Windows gets .exe in the staged file, files[] and distribution.json', (t) => {
  const windows = kitPlatforms.filter((platform) => platform.goos === 'windows');
  if (windows.length === 0) {
    t.skip('the kit cannot build Windows');
    return;
  }
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-win32-pack-'));
  t.after(() => fs.rmSync(temp, { recursive: true, force: true }));

  const product = JSON.parse(fs.readFileSync(productFile, 'utf8'));
  product.platforms = windows;
  const file = path.join(temp, 'npm', 'product.json');
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, `${JSON.stringify(product, null, 2)}\n`);

  git(temp, 'init');
  git(temp, 'config', 'user.name', 'Packaging Test');
  git(temp, 'config', 'user.email', 'packaging-test@example.invalid');
  git(temp, 'add', 'npm/product.json');
  git(temp, 'commit', '-m', 'test: initialize fixture');

  const binaries = path.join(temp, 'input');
  writeFixtureBinaries(binaries, windows);
  // The fixtures themselves must carry the suffix, which is the first thing that
  // would break: the kit looks for mexc.exe and a plain 'mexc' is simply missing.
  for (const platform of windows) {
    assert.ok(
      fs.existsSync(path.join(binaries, platform.id, 'mexc.exe')),
      `${platform.id}: fixture was not written as mexc.exe`,
    );
  }

  const output = path.join(temp, 'output');
  const result = childProcess.spawnSync(process.execPath, [path.join(kitRoot, 'package.mjs'),
    '--product', file,
    '--output', output,
    '--snapshot',
    '--version', '1.2.3-beta.1',
    '--skip-build',
    '--binaries-dir', binaries,
  ], { cwd: temp, encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);

  const distribution = JSON.parse(fs.readFileSync(path.join(output, 'stage', 'main', 'lib', 'distribution.json'), 'utf8'));
  for (const platform of windows) {
    const packageJSON = JSON.parse(fs.readFileSync(path.join(output, 'stage', platform.id, 'package.json'), 'utf8'));
    assert.ok(
      fs.existsSync(path.join(output, 'stage', platform.id, 'bin', 'mexc.exe')),
      `${platform.id}: staged binary is not named mexc.exe`,
    );
    assert.deepEqual(packageJSON.files, ['bin/mexc.exe'], `${platform.id}: files[] does not list mexc.exe`);
    assert.equal(
      distribution.platforms[platform.id].binary,
      'bin/mexc.exe',
      `${platform.id}: distribution.json points the launcher at the wrong file`,
    );
    assert.deepEqual(packageJSON.os, ['win32']);
  }
});

// The counterpart of the three requirePosixPackagingHost skips above: where they
// cannot run, this asserts WHY, on the platform that produces it. Without it the
// refusal would be the one behaviour covered nowhere — asserted by the skips'
// comments and by nothing that executes.
//
// It runs the real product, whose matrix declares POSIX platforms, and does not
// need a binary or a git tag: the refusal comes before either is read.
test('npm packaging refuses a POSIX target on a Windows host', (t) => {
  if (process.platform !== 'win32') {
    t.skip('the refusal exists only on a Windows host');
    return;
  }
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-refuse-'));
  t.after(() => fs.rmSync(temp, { recursive: true, force: true }));

  const result = childProcess.spawnSync(process.execPath, [
    path.join(kitRoot, 'package.mjs'),
    '--product', productFile,
    '--output', path.join(temp, 'output'),
    '--snapshot',
    '--version', '1.2.3-beta.1',
    '--skip-build',
    '--binaries-dir', temp,
  ], { cwd: repo, encoding: 'utf8' });

  assert.notEqual(result.status, 0, 'packaging a POSIX target on Windows must be refused');
  assert.match(result.stderr, /npm packaging cannot run on Windows/);
  // It must name the platforms it refused and the way out, or the caller is told
  // "no" with nothing to do about it.
  assert.match(result.stderr, /darwin-arm64/);
  assert.match(result.stderr, /Run the packaging modes on Linux or macOS/);
  // And it must NOT claim the whole toolchain is unavailable: cross-compiling
  // works here, which is the sentence that keeps this from reading as "Windows is
  // unsupported".
  assert.match(result.stderr, /Cross-compiling for any target/);
});
