'use strict';

const fs = require('node:fs');
const path = require('node:path');
const childProcess = require('node:child_process');
const { network } = require('./errors.cjs');

// Running the package manager on Windows takes a shell, and a shell is the whole
// problem — so the command is resolved to an absolute path FIRST and the shell
// never gets to choose what runs.
//
// Why a shell at all: the manager is `npm.cmd` / `pnpm.cmd` there, and Node has
// refused to spawn a .cmd without one since 18.20.2 — an outright EINVAL, not a
// fallback.
//
// Why that is dangerous by itself: `cmd.exe` resolves a bare command name against
// the CURRENT DIRECTORY before PATH. A file named npm.cmd, npm.bat or npm.com in
// whatever directory the user happens to be in would win — and this runs from the
// update notifier, detached, with output discarded, on ordinary commands like
// `doctor`. The user would never see it.
//
// So resolveOnPath does the lookup this code is willing to vouch for: PATH only,
// PATHEXT order, empty and "." entries dropped because both mean the current
// directory. The result is an absolute path, quoted, and cmd.exe is left with
// nothing to search for.
//
// Arguments are a separate question and the answer is that nothing a user types
// reaches them: `update` accepts exactly two flags and rejects every other
// argument (parseFlags); the manager comes from a literal list; packageName is
// stamped into distribution.json at build time; the registry argument is either
// ABSENT — the usual case, and what lets npm apply the user's own configuration —
// or this file's own PUBLIC_REGISTRY constant; and the one value that arrives from
// the network is the semver-NORMALISED version, not the raw string the registry
// sent. Adding an argument that carries user input would break that and needs this
// comment revisited rather than extended.
function resolveOnPath(command, env = process.env, platform = process.platform) {
  if (platform !== 'win32') return command;
  const extensions = String(env.PATHEXT || '.COM;.EXE;.BAT;.CMD')
    .split(';')
    .map((value) => value.trim())
    .filter(Boolean);
  const directories = String(env.PATH || env.Path || '')
    .split(path.delimiter)
    // '' and '.' are both "the current directory" to Windows. Dropping them is
    // the point of this function, not an edge case.
    .filter((value) => value && value !== '.');
  for (const directory of directories) {
    for (const extension of extensions) {
      const candidate = path.join(directory, command + extension);
      try {
        if (fs.statSync(candidate).isFile()) return candidate;
      } catch (_) { /* Not here; keep looking. */ }
    }
  }
  return null;
}

function run(command, args, options = {}) {
  const env = options.env || process.env;
  const windows = process.platform === 'win32';
  const resolved = resolveOnPath(command, env);
  if (resolved === null) {
    // Shaped like a spawnSync failure because every caller already branches on
    // `result.error`, and "no package manager here" is exactly that kind of miss.
    return { error: new Error(`${command} was not found on PATH`), status: null, stdout: '', stderr: '' };
  }
  return childProcess.spawnSync(windows ? `"${resolved}"` : resolved, args, {
    encoding: 'utf8',
    windowsHide: true,
    // With an absolute quoted path there is nothing left for the shell to resolve;
    // it is here only because a .cmd cannot be executed without one.
    shell: windows,
    timeout: options.timeout || 10_000,
    env,
  });
}

function packagePath(globalRoot, packageName) {
  return path.join(globalRoot, ...packageName.split('/'));
}

function sameRealPath(left, right) {
  try {
    return fs.realpathSync(left) === fs.realpathSync(right);
  } catch (_) {
    return false;
  }
}

function managerFromUserAgent(env = process.env) {
  const value = env.npm_config_user_agent || '';
  if (value.startsWith('pnpm/')) return 'pnpm';
  if (value.startsWith('npm/')) return 'npm';
  return null;
}

function globalInstallation(mainRoot, packageName, runner = run) {
  for (const manager of ['npm', 'pnpm']) {
    const result = runner(manager, ['root', '--global'], { timeout: 5_000 });
    if (result.error || result.status !== 0) continue;
    const root = String(result.stdout || '').trim();
    if (root && sameRealPath(packagePath(root, packageName), mainRoot)) {
      return { mode: 'global', manager, globalRoot: root };
    }
  }
  return null;
}

function detectInstallation(mainRoot, packageName, env = process.env, runner = run) {
  const global = globalInstallation(mainRoot, packageName, runner);
  if (global) return global;
  const normalized = mainRoot.split(path.sep).join('/');
  const manager = managerFromUserAgent(env) || 'npm';
  if (normalized.includes('/_npx/') || env.npm_command === 'exec') {
    return { mode: 'npx', manager };
  }
  if (normalized.includes('/node_modules/')) {
    return { mode: 'local', manager };
  }
  return { mode: 'unknown', manager };
}

function parseViewVersion(stdout) {
  const raw = String(stdout || '').trim();
  if (!raw) return '';
  try {
    const parsed = JSON.parse(raw);
    if (typeof parsed === 'string') return parsed;
    if (Array.isArray(parsed) && typeof parsed[parsed.length - 1] === 'string') return parsed[parsed.length - 1];
  } catch (_) {
    return raw.replace(/^['"]|['"]$/g, '');
  }
  return '';
}

// The official npm registry: the ONE address this kit carries, and the fallback
// for a version query the user's own configuration cannot answer.
//
// Deliberately not the registry the package was published to. Baking that in
// pointed every user's update check at the publisher's network — wrong for anyone
// who reaches npm through a mirror or a proxy — and it printed a hostname into an
// artifact that ships to whoever installs it. A public default is wrong for
// nobody: it either answers or it 404s, and `--registry` covers the case where
// the package is somewhere else.
const PUBLIC_REGISTRY = 'https://registry.npmjs.org/';

// A null candidate means "pass no --registry", and it is deliberately FIRST.
//
// An explicit --registry outranks the user's .npmrc, npm_config_registry and any
// @scope:registry override, so passing the published address unconditionally is
// how `update` came to ignore the registry the user installed from and fail off
// the publisher's network.
//
// Omitting the argument is not a guess at that registry — it is the only way to
// use it. What npm applies with no --registry is by definition what an install
// with no --registry used, along with the matching auth token, which reading a
// registry out of the config and passing it back would break for a scoped
// package.
//
// It has to be inferred this way because npm exposes no SUPPORTED record of the
// address an install used: measured on npm 10.8.2 against a global install, there
// is no hidden .package-lock.json under `npm root -g` nor in the main package's
// own node_modules, no `resolved` in `npm ls -g --json --long`, and no `_resolved`
// in the installed package.json.
//
// The origin does survive in npm's HTTP cache index, which keys entries by fetch
// URL. That is not a contract and must not become one: the key format belongs to
// make-fetch-happen, `npm cache clean` erases it, and it accumulates every origin
// ever fetched for a name with nothing marking which one was the install.
//
// The public registry is the second candidate, for the machine whose npm has no
// opinion or the wrong one.
//
// Returns { version, registry } — the candidate that answered, so the install, the
// rollback command and the printed guidance all name where the version was read
// from rather than somewhere it might not exist.
function queryVersion(manager, packageName, channel, runner = run, timeout = 15_000) {
  const spec = `${packageName}@${channel}`;
  for (const candidate of [null, PUBLIC_REGISTRY]) {
    const args = ['view', spec, 'version', '--json'];
    if (candidate) args.push('--registry', candidate);
    const result = runner(manager, args, { timeout });
    if (result.error || result.status !== 0) continue;
    const version = parseViewVersion(result.stdout);
    if (version) return { version, registry: candidate };
  }
  throw network(
    `cannot resolve ${spec} from the registry your package manager is configured to use, nor from ${PUBLIC_REGISTRY}`,
    `check that registry is reachable and serves this package: \`${manager} view ${spec} version\``,
  );
}

function installExact(manager, packageName, version, registry, runner = run) {
  const spec = `${packageName}@${version}`;
  const args = manager === 'pnpm' ? ['add', '--global', spec] : ['install', '--global', spec];
  if (registry) args.push('--registry', registry);
  return runner(manager, args, { timeout: 10 * 60_000 });
}

function exactInstallCommand(manager, packageName, version, registry, local = false) {
  const spec = `${packageName}@${version}`;
  const where = registry ? ` --registry ${registry}` : '';
  if (manager === 'pnpm') {
    return `pnpm add ${local ? '--save-exact' : '--global'} ${spec}${where}`;
  }
  return `npm install ${local ? '--save-exact' : '--global'} ${spec}${where}`;
}

function exactNPXCommand(packageName, version, commandName, registry) {
  const where = registry ? `--registry ${registry} ` : '';
  return `npx --yes ${where}--package ${packageName}@${version} -- ${commandName} --version`;
}

module.exports = {
  PUBLIC_REGISTRY,
  resolveOnPath,
  run,
  packagePath,
  managerFromUserAgent,
  globalInstallation,
  detectInstallation,
  parseViewVersion,
  queryVersion,
  installExact,
  exactInstallCommand,
  exactNPXCommand,
};
