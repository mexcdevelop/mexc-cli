'use strict';

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const childProcess = require('node:child_process');
const semver = require('./semver.cjs');

// 2: the registry left the cache identity when it stopped being a property of the
// build. It is now resolved per run (the user's own configuration first), so there
// is no fixed address to key on, and a cache written by version 1 is discarded by
// the schemaVersion check in matches() rather than migrated.
const SCHEMA_VERSION = 2;
const LEASE_MAX_AGE_MS = 5 * 60_000;

function paths(config, home = os.homedir()) {
  const directory = path.join(home, config.stateDir);
  return {
    directory,
    cache: path.join(directory, 'update-notifier.json'),
    lease: path.join(directory, 'update-notifier.lock'),
  };
}

function matches(cache, config, channel) {
  return cache && cache.schemaVersion === SCHEMA_VERSION &&
    cache.package === config.packageName &&
    cache.channel === channel && cache.current === config.version &&
    typeof cache.checkedAt === 'string' && typeof cache.latest === 'string';
}

function read(config, channel, home) {
  const file = paths(config, home).cache;
  try {
    const cache = JSON.parse(fs.readFileSync(file, 'utf8'));
    return matches(cache, config, channel) ? cache : null;
  } catch (_) {
    return null;
  }
}

function write(config, channel, latestVersion, checkedAt = new Date(), home) {
  const target = paths(config, home);
  fs.mkdirSync(target.directory, { recursive: true, mode: 0o700 });
  const value = {
    schemaVersion: SCHEMA_VERSION,
    package: config.packageName,
    channel,
    checkedAt: checkedAt.toISOString(),
    current: config.version,
    latest: latestVersion || '',
  };
  const temp = `${target.cache}.${process.pid}.${Date.now()}.tmp`;
  try {
    fs.writeFileSync(temp, `${JSON.stringify(value)}\n`, { mode: 0o600 });
    fs.renameSync(temp, target.cache);
    try { fs.chmodSync(target.cache, 0o600); } catch (_) { /* Windows may ignore POSIX modes. */ }
  } finally {
    try { fs.unlinkSync(temp); } catch (_) { /* Atomic rename already consumed it. */ }
  }
  return value;
}

function fresh(cache, config, now = Date.now()) {
  if (!cache) return false;
  const checked = Date.parse(cache.checkedAt);
  return Number.isFinite(checked) && now - checked < config.refreshMs;
}

function acquireLease(config, home, now = Date.now()) {
  const target = paths(config, home);
  fs.mkdirSync(target.directory, { recursive: true, mode: 0o700 });
  const attempt = () => {
    const fd = fs.openSync(target.lease, 'wx', 0o600);
    fs.writeFileSync(fd, `${process.pid}\n`);
    fs.closeSync(fd);
    return target.lease;
  };
  try {
    return attempt();
  } catch (error) {
    if (error.code !== 'EEXIST') return null;
    try {
      if (now - fs.statSync(target.lease).mtimeMs > LEASE_MAX_AGE_MS) {
        fs.unlinkSync(target.lease);
        return attempt();
      }
    } catch (_) { /* Another process won the race. */ }
    return null;
  }
}

function releaseLease(config, home) {
  try { fs.unlinkSync(paths(config, home).lease); } catch (_) { /* Best effort. */ }
}

function eligible(config, args, env = process.env, stderr = process.stderr) {
  if (!stderr.isTTY || env.CI || env.MEXC_CLI_NO_UPDATE_NOTIFIER === '1') return false;
  if (!semver.parse(config.version)) return false;
  const first = args[0] || '';
  return !['update', 'completion', 'mcp'].includes(first);
}

function maybeNotify(config, args, options = {}) {
  const env = options.env || process.env;
  const stderr = options.stderr || process.stderr;
  if (!eligible(config, args, env, stderr)) return;
  const channel = semver.channel(config.version);
  const current = read(config, channel, options.home);
  if (current && semver.parse(current.latest) && semver.compare(current.latest, config.version) > 0) {
    stderr.write(`Update available: ${config.version} -> ${current.latest}. Run \`${config.commandName} update\`.\n`);
  }
  if (fresh(current, config, options.now || Date.now())) return;
  const lease = acquireLease(config, options.home, options.now || Date.now());
  if (!lease) return;
  const spawn = options.spawn || childProcess.spawn;
  try {
    const helper = path.join(__dirname, 'refresh.cjs');
    const child = spawn(process.execPath, [helper, path.join(__dirname, 'distribution.json')], {
      detached: true,
      stdio: 'ignore',
      windowsHide: true,
      env: { ...env, MEXC_CLI_REFRESH_LEASE: lease },
    });
    child.unref();
  } catch (_) {
    releaseLease(config, options.home);
  }
}

module.exports = {
  SCHEMA_VERSION,
  paths,
  read,
  write,
  fresh,
  acquireLease,
  releaseLease,
  eligible,
  maybeNotify,
};
