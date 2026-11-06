#!/usr/bin/env node

import childProcess from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const semver = require('./runtime/lib/semver.cjs');
const supportedPlatforms = JSON.parse(fs.readFileSync(path.join(here, 'platforms.json'), 'utf8'));
const MAX_MAIN_UNPACKED_BYTES = 1024 * 1024;
const MAX_PLATFORM_UNPACKED_BYTES = 128 * 1024 * 1024;

function fail(message) {
  throw new Error(message);
}

function run(command, args, options = {}) {
  const result = childProcess.spawnSync(command, args, {
    cwd: options.cwd,
    env: options.env || process.env,
    encoding: 'utf8',
    stdio: options.stdio || ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });
  if (result.error || result.status !== 0) {
    const detail = String(result.stderr || result.stdout || result.error?.message || '').trim();
    fail(`${command} ${args.join(' ')} failed${detail ? `: ${detail}` : ''}`);
  }
  return String(result.stdout || '').trim();
}

function parseArgs(argv) {
  const out = { skipBuild: false, snapshot: false };
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === '--skip-build') out.skipBuild = true;
    else if (arg === '--snapshot') out.snapshot = true;
    else if (['--product', '--output', '--version', '--binaries-dir'].includes(arg)) {
      if (!argv[index + 1]) fail(`${arg} requires a value`);
      out[arg.slice(2).replace(/-([a-z])/g, (_, value) => value.toUpperCase())] = argv[++index];
    } else {
      fail(`unknown argument ${arg}`);
    }
  }
  if (!out.product) fail('--product is required');
  if (out.skipBuild && !out.binariesDir) fail('--skip-build requires --binaries-dir');
  if (out.binariesDir && !out.skipBuild) fail('--binaries-dir requires --skip-build');
  if (out.snapshot && !out.version) fail('--snapshot requires --version');
  if (out.version && !out.snapshot) fail('--version requires --snapshot');
  return out;
}

const LICENSES = ['MIT', 'UNLICENSED'];
const REPOSITORY_URL = /^git\+https:\/\/[A-Za-z0-9.-]+\/[A-Za-z0-9._-]+\/[A-Za-z0-9._-]+\.git$/;

// repositoryField is spread into each generated package.json: present only when
// the product declares a repository, so a product that does not is unchanged.
function repositoryField(product) {
  return product.repository ? { repository: { type: 'git', url: product.repository } } : {};
}

// copyLicense puts the repository's LICENSE in a package root. npm includes a
// root LICENSE whatever `files` says, so this is the whole of shipping it. A
// repository without one (a proprietary downstream) ships none, as before.
function copyLicense(repo, root) {
  const source = path.join(repo, 'LICENSE');
  if (fs.existsSync(source)) fs.copyFileSync(source, path.join(root, 'LICENSE'));
}

function loadProduct(file) {
  const value = JSON.parse(fs.readFileSync(file, 'utf8'));
  for (const field of ['packageName', 'commandName', 'binaryName', 'stateDir', 'license', 'nodeRange', 'goEntry', 'buildInfoPackage']) {
    if (typeof value[field] !== 'string' || !value[field]) fail(`product field ${field} is required`);
  }
  if (!/^(?:@[a-z0-9][a-z0-9._-]*\/)?[a-z0-9][a-z0-9._-]*$/.test(value.packageName)) {
    fail('packageName must be a lowercase npm package');
  }
  if (!/^[a-z0-9][a-z0-9-]*$/.test(value.commandName)) fail('commandName must be lowercase and hyphenated');
  if (path.basename(value.stateDir) !== value.stateDir) fail('stateDir must be one directory name');
  // The two licenses this kit has a reason to ship. MIT is the published
  // product's; UNLICENSED stays legal because a downstream repository consumes
  // this kit as a synced copy and keeps its own product proprietary.
  if (!LICENSES.includes(value.license)) fail(`license must be one of ${LICENSES.join(', ')}`);
  // Optional: where the source lives. npm trusted publishing compares it with the
  // repository the publishing workflow runs in and refuses a mismatch, so a
  // product published from GitHub has to declare it. It is written verbatim into
  // every package.json, so only the git+https form npm itself writes is accepted.
  if (value.repository !== undefined && !REPOSITORY_URL.test(value.repository)) {
    fail('repository must look like git+https://<host>/<owner>/<repo>.git');
  }
  if (value.nodeRange !== '>=20') fail('nodeRange must be >=20');
  // A SUBSET, not an equality. The check exists to stop a product naming a
  // platform this kit has no build recipe for, and a subset still guarantees
  // that. Equality guaranteed something else nobody asked for: that every
  // product ships every platform the kit knows. That coupling has a hard edge —
  // a downstream repository consumes this kit as a synced copy (see
  // scripts/upgrade-shared.sh), so widening the matrix here used to break its
  // packaging until it widened too, and it could not opt out of a platform it
  // does not want to support.
  //
  // What equality DID catch and a subset does not: a product that silently
  // loses a platform still packages cleanly, it just stops shipping it. That
  // protection moves to test/product-matrix.test.cjs, which names each
  // product's expected platforms explicitly.
  if (!Array.isArray(value.platforms) || value.platforms.length === 0) {
    fail('product platforms must be a non-empty array');
  }
  const supported = new Map(supportedPlatforms.map((platform) => [platform.id, platform]));
  const declared = new Set();
  for (const platform of value.platforms) {
    const id = platform && platform.id;
    if (typeof id !== 'string' || !id) fail('every product platform needs a string id');
    if (declared.has(id)) fail(`product platform ${id} is declared twice`);
    declared.add(id);
    const kit = supported.get(id);
    if (!kit) {
      fail(`product platform ${id} is not one this kit can build (it knows: ${[...supported.keys()].sort().join(', ')})`);
    }
    // Compared field by field rather than by serialising the objects: a
    // whole-object compare is sensitive to key order, which is invisible in a
    // diff and has nothing to do with whether the entry is right.
    for (const field of ['nodePlatform', 'nodeArch', 'goos', 'goarch']) {
      if (platform[field] !== kit[field]) {
        fail(`product platform ${id} sets ${field}=${JSON.stringify(platform[field])} but this kit builds it as ${JSON.stringify(kit[field])}`);
      }
    }
  }
  return value;
}

function git(repo, args) {
  return run('git', args, { cwd: repo });
}

function packagingVersion(repo, explicit, snapshot) {
  if (snapshot) {
    const parsed = semver.parse(explicit);
    if (!parsed) fail(`invalid Semantic Version ${explicit}`);
    if (parsed.prerelease.length === 0) {
      fail(`snapshot version ${explicit} must include a prerelease identifier, for example ${explicit}-beta.1`);
    }
    return { version: explicit, tag: null, mode: 'snapshot' };
  }
  const tags = git(repo, ['tag', '--points-at', 'HEAD']).split(/\r?\n/).filter(Boolean);
  const validTags = tags.filter((tag) => tag.startsWith('v') && semver.parse(tag.slice(1)));
  if (validTags.length !== 1) {
    fail(`HEAD does not have exactly one valid release tag vMAJOR.MINOR.PATCH[-prerelease] (found: ${tags.join(', ') || '<none>'})`);
  }
  const tag = validTags[0];
  return { version: tag.slice(1), tag, mode: 'release' };
}

function sha256(file) {
  return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
}

function packageName(product, platform) {
  return `${product.packageName}-${platform.id}`;
}

// The file name the binary is written and shipped under. Windows will not run a
// file without an executable extension, so the suffix is not cosmetic — and it
// has to be decided per PLATFORM rather than per product, because one build
// produces artefacts for all of them.
//
// Nothing at runtime reads this back by name: the launcher resolves the binary
// through the `binary` path recorded per platform in distribution.json, which is
// built from this. So the suffix has exactly one source.
function binaryName(product, platform) {
  return platform.goos === 'windows' ? `${product.binaryName}.exe` : product.binaryName;
}

function safeOutput(output, repo, fixture) {
  const resolved = path.resolve(output);
  if (resolved === path.parse(resolved).root || resolved === os.homedir()) fail(`unsafe output directory ${resolved}`);
  const releaseOutputs = [
    path.join(repo, 'dist', 'npm', 'scoped'),
    path.join(repo, 'dist', 'npm', 'unscoped'),
  ];
  if (!fixture && !releaseOutputs.includes(resolved)) {
    fail('release output must be a repository dist/npm package variant directory');
  }
  return resolved;
}

function reportsVersion(output, expected) {
  const match = /^\S+\s+version\s+(\S+)/m.exec(String(output || '').trim());
  return Boolean(match && match[1] === expected);
}

function buildBinaries(repo, product, version, output, commit, date) {
  const directory = path.join(output, 'binaries');
  fs.mkdirSync(directory, { recursive: true });
  for (const platform of product.platforms) {
    const targetDir = path.join(directory, platform.id);
    fs.mkdirSync(targetDir, { recursive: true });
    const binary = path.join(targetDir, binaryName(product, platform));
    const flags = `-s -w -X ${product.buildInfoPackage}.Version=${version} -X ${product.buildInfoPackage}.Commit=${commit} -X ${product.buildInfoPackage}.Date=${date}`;
    run('go', ['build', '-mod=vendor', '-trimpath', '-ldflags', flags, '-o', binary, product.goEntry], {
      cwd: repo,
      env: {
        ...process.env,
        GOOS: platform.goos,
        GOARCH: platform.goarch,
        CGO_ENABLED: '0',
        GOTOOLCHAIN: 'local',
        GOPROXY: 'off',
        GOSUMDB: 'off',
      },
    });
    fs.chmodSync(binary, 0o755);
    if (platform.nodePlatform === process.platform && platform.nodeArch === process.arch) {
      const text = run(binary, ['--version']);
      if (!reportsVersion(text, version)) fail(`${platform.id} binary did not report version ${version}: ${text}`);
    }
  }
  return directory;
}

function copyRuntime(target) {
  fs.cpSync(path.join(here, 'runtime'), target, { recursive: true });
  // Go's module cache is intentionally read-only. Module consumers may resolve
  // this kit from that cache, so cpSync can preserve non-writable directory modes
  // in the staging tree. Make only the generated copy writable before adding
  // distribution.json and package.json; the cached source stays intact.
  fs.chmodSync(target, 0o755);
  fs.chmodSync(path.join(target, 'bin'), 0o755);
  fs.chmodSync(path.join(target, 'lib'), 0o755);
  fs.chmodSync(path.join(target, 'bin', 'launcher.cjs'), 0o755);
}

function stagePackages(repo, product, version, output, binariesDir) {
  const stage = path.join(output, 'stage');
  fs.mkdirSync(stage, { recursive: true });
  const platformMap = {};
  const staged = [];
  for (const platform of product.platforms) {
    const name = packageName(product, platform);
    const root = path.join(stage, platform.id);
    const targetBinary = binaryName(product, platform);
    const source = path.join(binariesDir, platform.id, targetBinary);
    if (!fs.statSync(source).isFile()) fail(`missing binary ${source}`);
    fs.mkdirSync(path.join(root, 'bin'), { recursive: true });
    fs.copyFileSync(source, path.join(root, 'bin', targetBinary));
    fs.chmodSync(path.join(root, 'bin', targetBinary), 0o755);
    // Guarded on the TARGET, not the host. A windows binary has no executable bit
    // to carry — the extension is what makes it runnable — so there is nothing
    // here to verify, and npm records whatever mode the host happened to give the
    // file.
    //
    // It has to be the target, because a windows-ONLY product legitimately
    // packages on a Windows host: assertHostCanPack returns early when the matrix
    // contains no POSIX platform. This check then read the synthesised 0666 that
    // every Windows file reports, found no 0o111, and refused the one build the
    // refusal above deliberately allows — so the comment that used to stand here,
    // claiming this was "reachable only on a POSIX host", was wrong in exactly
    // that case. Measured: `--product` with only win32 platforms failed with
    // "platform binary is not executable: ...\win32-arm64\mexc.exe".
    //
    // Kept for every POSIX target, on any host: a binary that arrives without +x
    // installs dead, and that is a real defect worth stopping for.
    if (platform.goos !== 'windows' && (fs.statSync(path.join(root, 'bin', targetBinary)).mode & 0o111) === 0) {
      fail(`platform binary is not executable: ${source}`);
    }
    fs.writeFileSync(path.join(root, 'package.json'), `${JSON.stringify({
      name,
      version,
      description: `${product.commandName} binary for ${platform.nodePlatform} ${platform.nodeArch}`,
      license: product.license,
      ...repositoryField(product),
      os: [platform.nodePlatform],
      cpu: [platform.nodeArch],
      files: [`bin/${targetBinary}`],
      engines: { node: product.nodeRange },
      preferUnplugged: true,
    }, null, 2)}\n`);
    copyLicense(repo, root);
    platformMap[platform.id] = { packageName: name, binary: `bin/${targetBinary}` };
    staged.push({ kind: 'platform', id: platform.id, root, name, binary: path.join(root, 'bin', targetBinary) });
  }
  const mainRoot = path.join(stage, 'main');
  copyRuntime(mainRoot);
  fs.writeFileSync(path.join(mainRoot, 'lib', 'distribution.json'), `${JSON.stringify({
    schemaVersion: 1,
    packageName: product.packageName,
    commandName: product.commandName,
    binaryName: product.binaryName,
    stateDir: product.stateDir,
    version,
    // No registry. The address a package was published to is not a fact about the
    // package, and shipping it made every update check point at the publisher's
    // network. The launcher resolves the registry per run — the user's own npm
    // configuration first, the public registry as the fallback.
    refreshMs: 60 * 60_000,
    platforms: platformMap,
  }, null, 2)}\n`, { mode: 0o600 });
  fs.writeFileSync(path.join(mainRoot, 'package.json'), `${JSON.stringify({
    name: product.packageName,
    version,
    description: `${product.commandName} command-line interface`,
    license: product.license,
    ...repositoryField(product),
    type: 'commonjs',
    bin: { [product.commandName]: 'bin/launcher.cjs' },
    files: ['bin', 'lib'],
    engines: { node: product.nodeRange },
    optionalDependencies: Object.fromEntries(product.platforms.map((platform) => [packageName(product, platform), version])),
  }, null, 2)}\n`);
  copyLicense(repo, mainRoot);
  staged.push({ kind: 'main', id: 'main', root: mainRoot, name: product.packageName });
  return staged;
}

function scanStage(item) {
  const forbiddenNames = new Set(['.npmrc', '.env', 'go.mod', 'go.sum']);
  const sensitive = /(npm_[A-Za-z0-9]{20,}|_authToken\s*=|BEGIN [A-Z ]*PRIVATE KEY|[A-Z][A-Z0-9_]*_(?:ACCESS_)?TOKEN\s*=)/;
  const walk = (directory) => {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      if (forbiddenNames.has(entry.name)) fail(`forbidden file ${entry.name} in ${item.name}`);
      const full = path.join(directory, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (/\.(?:json|cjs|js|md|txt)$/.test(entry.name) && sensitive.test(fs.readFileSync(full, 'utf8'))) {
        fail(`sensitive content detected in ${full}`);
      }
    }
  };
  walk(item.root);
  const manifest = JSON.parse(fs.readFileSync(path.join(item.root, 'package.json'), 'utf8'));
  if (manifest.scripts) fail(`${item.name} must not contain lifecycle scripts`);
}

// npmInvocation returns the command and leading arguments that run npm with no
// shell involved.
//
// On Windows the only `npm` on PATH is a shim, npm.CMD, and Node refuses to spawn
// a .cmd without a shell: spawnSync answers EINVAL (the CVE-2024-27980 fix).
// Measured on Node v24.19.0 — before this, packaging a windows-only product on a
// Windows host died with "spawnSync npm ENOENT".
//
// The runtime's resolver answers the same problem with `shell: true` over a quoted
// absolute path, and its own comment says that is safe only because nothing a user
// types reaches its arguments. THIS file does not meet that condition:
// --pack-destination carries a path derived from --output. cmd.exe would
// re-interpret &, ^ and " inside it.
//
// So no shell at all. npm ships its own entry script beside the shim, and running
// that under the Node already executing this file is the same program with nothing
// left to quote. Falls back to the bare name when the script is not where npm puts
// it, which leaves the POSIX path and any unusual install exactly as they were.
function npmInvocation(env = process.env) {
  if (process.platform !== 'win32') return { command: 'npm', prefix: [] };
  // Beside the node running THIS file, first. It is both the most reliable place
  // to look and the right pairing — the npm that ships with an interpreter
  // belongs to it. Scanning PATH for npm.cmd and inferring the library path from
  // its directory holds for the official installer, but not for the version
  // managers that put their shims somewhere else entirely.
  const beside = path.join(path.dirname(process.execPath), 'node_modules', 'npm', 'bin', 'npm-cli.js');
  if (fs.existsSync(beside)) return { command: process.execPath, prefix: [beside] };
  const extensions = String(env.PATHEXT || '.COM;.EXE;.BAT;.CMD')
    .split(';')
    .map((value) => value.trim())
    .filter(Boolean);
  for (const directory of String(env.PATH || env.Path || '').split(path.delimiter).filter(Boolean)) {
    for (const extension of extensions) {
      if (!fs.existsSync(path.join(directory, `npm${extension}`))) continue;
      const cli = path.join(directory, 'node_modules', 'npm', 'bin', 'npm-cli.js');
      if (fs.existsSync(cli)) return { command: process.execPath, prefix: [cli] };
    }
  }
  return { command: 'npm', prefix: [] };
}

function pack(staged, output) {
  const tarballs = path.join(output, 'tarballs');
  const npmCache = path.join(output, '.npm-cache');
  fs.mkdirSync(tarballs, { recursive: true });
  const results = [];
  const npm = npmInvocation();
  try {
    for (const item of staged) {
      scanStage(item);
      const raw = run(npm.command, [...npm.prefix, 'pack', '--json', '--pack-destination', tarballs], {
        cwd: item.root,
        env: { ...process.env, npm_config_cache: npmCache },
      });
      const parsed = JSON.parse(raw);
      if (!Array.isArray(parsed) || parsed.length !== 1) fail(`unexpected npm pack output for ${item.name}`);
      const packed = parsed[0];
      if (packed.name !== item.name || packed.version !== versionFromPackage(item.root)) {
        fail(`packed identity mismatch for ${item.name}: ${packed.name}@${packed.version}`);
      }
      const files = packed.files.map((entry) => entry.path).sort();
      // LICENSE is the one root file besides package.json: copyLicense puts it
      // there on purpose, and npm would include it regardless of `files`.
      const rootFile = (file) => file === 'package.json' || file === 'LICENSE';
      const allowed = item.kind === 'main'
        ? files.every((file) => rootFile(file) || file.startsWith('bin/') || file.startsWith('lib/'))
        : files.every((file) => rootFile(file) || file.startsWith('bin/'));
      if (!allowed) fail(`unexpected files in ${item.name}: ${files.join(', ')}`);
      const maxUnpacked = item.kind === 'main' ? MAX_MAIN_UNPACKED_BYTES : MAX_PLATFORM_UNPACKED_BYTES;
      if (packed.unpackedSize > maxUnpacked || packed.size > MAX_PLATFORM_UNPACKED_BYTES) {
        fail(`${item.name} exceeds the package size limit`);
      }
      results.push({
        name: packed.name,
        version: packed.version,
        filename: packed.filename,
        size: packed.size,
        unpackedSize: packed.unpackedSize,
        shasum: packed.shasum,
        integrity: packed.integrity,
        files,
      });
    }
  } finally {
    fs.rmSync(npmCache, { recursive: true, force: true });
  }
  return results;
}

function versionFromPackage(root) {
  return JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8')).version;
}

// assertHostCanPack refuses to package on a host that cannot give a POSIX binary
// its executable bit.
//
// This is a REFUSAL, not a missing feature to work around. `npm pack` takes each
// entry's mode from the filesystem, and Windows has no executable bit to give
// it: measured here, a file chmod'ed 0o755 stats as 0o666 and npm packs it as
// `-rw-r--r--`. A darwin or linux tarball built on Windows therefore installs a
// binary the launcher cannot start, and nothing in the packaging output says so
// — the tarball is well-formed, correctly named, the right size, and dead.
//
// Failing here rather than at the per-platform check in stagePackages is the
// point. That check fires on the FIRST platform in the matrix, so the error read
// "platform binary is not executable: ...binaries/darwin-arm64/mexc" — naming a
// platform that has nothing to do with the cause, after minutes of
// cross-compiling six targets. This costs nothing and says what is wrong.
//
// Cross-COMPILING on Windows is unaffected and still works for every target;
// only the npm packaging step needs a POSIX host.
function assertHostCanPack(product) {
  if (process.platform !== 'win32') return;
  const posix = product.platforms.filter((platform) => platform.goos !== 'windows');
  if (posix.length === 0) return;
  fail(
    'npm packaging cannot run on Windows: `npm pack` reads each file\'s mode from the '
    + 'filesystem, and Windows has no executable bit to give it, so the '
    + `${posix.map((platform) => platform.id).join(', ')} `
    + 'binaries would ship as 0644 and fail to start once installed. '
    + 'Run the packaging modes on Linux or macOS. Cross-compiling for any target '
    + 'works here: `bash scripts/build.sh --target <goos>/<goarch>`.',
  );
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const productFile = path.resolve(args.product);
  const repo = path.dirname(path.dirname(productFile));
  const product = loadProduct(productFile);
  const packaging = packagingVersion(repo, args.version, args.snapshot);
  const { version } = packaging;
  const output = safeOutput(args.output || path.join(repo, 'dist', 'npm'), repo, args.skipBuild);
  // After argument validation, before anything expensive. A missing --version or
  // an unknown flag is wrong on every host and must be reported as itself;
  // putting the host check ahead of them made this answer "you are on Windows"
  // to a caller whose real mistake was the command line, and two rejection tests
  // caught exactly that.
  assertHostCanPack(product);
  for (const name of ['binaries', 'stage', 'tarballs']) fs.rmSync(path.join(output, name), { recursive: true, force: true });
  fs.mkdirSync(output, { recursive: true });
  const commit = git(repo, ['rev-parse', 'HEAD']);
  const date = git(repo, ['show', '-s', '--format=%cI', 'HEAD']);
  let binariesDir = args.binariesDir ? path.resolve(args.binariesDir) : path.join(output, 'binaries');
  if (!args.skipBuild) binariesDir = buildBinaries(repo, product, version, output, commit.slice(0, 12), date);
  const staged = stagePackages(repo, product, version, output, binariesDir);
  const packages = pack(staged, output);
  const binaries = staged.filter((item) => item.binary).map((item) => ({
    platform: item.id,
    file: path.relative(output, item.binary),
    size: fs.statSync(item.binary).size,
    sha256: sha256(item.binary),
  }));
  const manifest = {
    schemaVersion: 1,
    product: product.packageName,
    mode: packaging.mode,
    version,
    tag: packaging.tag,
    commit,
    date,
    platforms: product.platforms,
    binaries,
    packages,
    verification: {
      status: 'passed',
      packageCount: packages.length,
      exactOptionalDependencies: true,
      lifecycleDownloads: false,
      fileAllowlist: true,
      sensitiveContent: false,
      sizeLimits: true,
    },
  };
  fs.writeFileSync(path.join(output, 'release-manifest.json'), `${JSON.stringify(manifest, null, 2)}\n`);
  process.stdout.write(`${JSON.stringify({ output, manifest: path.join(output, 'release-manifest.json'), packages: packages.length })}\n`);
}

try {
  main();
} catch (error) {
  process.stderr.write(`error: ${error.message}\n`);
  process.exitCode = 1;
}
