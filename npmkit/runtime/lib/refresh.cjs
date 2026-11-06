'use strict';

const fs = require('node:fs');
const path = require('node:path');
const semver = require('./semver.cjs');
const cache = require('./cache.cjs');
const manager = require('./manager.cjs');

function load(file) {
  return JSON.parse(fs.readFileSync(file, 'utf8'));
}

function refresh(config, mainRoot = path.resolve(__dirname, '..'), env = process.env) {
  const channel = semver.channel(config.version);
  if (!channel) return;
  const previous = cache.read(config, channel);
  let latest = previous ? previous.latest : '';
  try {
    const installation = manager.detectInstallation(mainRoot, config.packageName, env);
    latest = manager.queryVersion(installation.manager, config.packageName, channel).version;
    if (!semver.parse(latest)) latest = previous ? previous.latest : '';
  } catch (_) {
    // A notifier failure is intentionally silent and throttled by checkedAt.
  }
  cache.write(config, channel, latest);
}

if (require.main === module) {
  try {
    const configFile = process.argv[2];
    if (configFile) refresh(load(configFile));
  } catch (_) {
    // Detached refreshes must never surface into the requested CLI command.
  } finally {
    try {
      const lease = process.env.MEXC_CLI_REFRESH_LEASE;
      if (lease) fs.unlinkSync(lease);
    } catch (_) { /* Best effort. */ }
  }
}

module.exports = { refresh };
