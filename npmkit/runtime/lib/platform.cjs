'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { createRequire } = require('node:module');
const { validation, internal } = require('./errors.cjs');

function key(platform = process.platform, arch = process.arch) {
  return `${platform}-${arch}`;
}

function selected(config, platform = process.platform, arch = process.arch) {
  const platformKey = key(platform, arch);
  const entry = config.platforms[platformKey];
  if (!entry) {
    throw validation(
      'failed_precondition',
      `unsupported platform ${platform}/${arch}`,
      `install ${config.packageName} on one of: ${Object.keys(config.platforms).sort().join(', ')}`,
    );
  }
  return entry;
}

function resolveBinary(config, mainRoot, platform = process.platform, arch = process.arch) {
  const entry = selected(config, platform, arch);
  const requireFromMain = createRequire(path.join(mainRoot, 'package.json'));
  let manifestPath;
  try {
    manifestPath = requireFromMain.resolve(`${entry.packageName}/package.json`);
  } catch (error) {
    throw validation(
      'failed_precondition',
      `the optional platform package ${entry.packageName} is not installed`,
      `reinstall ${config.packageName} without disabling optional dependencies`,
    );
  }
  let manifest;
  try {
    manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
  } catch (error) {
    if (error.code === 'ENOENT') {
      throw validation(
        'failed_precondition',
        `the optional platform package ${entry.packageName} is not installed`,
        `reinstall ${config.packageName} without disabling optional dependencies`,
      );
    }
    throw internal(`cannot read platform package manifest ${manifestPath}: ${error.message}`);
  }
  if (manifest.name !== entry.packageName || manifest.version !== config.version) {
    throw validation(
      'failed_precondition',
      `platform package version mismatch: ${manifest.name || entry.packageName}@${manifest.version || 'unknown'} does not match ${config.packageName}@${config.version}`,
      `reinstall the exact main package version with your package manager`,
    );
  }
  const binary = path.resolve(path.dirname(manifestPath), entry.binary);
  let stat;
  try {
    stat = fs.statSync(binary);
  } catch (error) {
    throw validation('failed_precondition', `platform binary is missing at ${binary}`, `reinstall ${entry.packageName}@${config.version}`);
  }
  if (!stat.isFile()) {
    throw validation('failed_precondition', `platform binary is not a regular file at ${binary}`, `reinstall ${entry.packageName}@${config.version}`);
  }
  return { binary, entry, manifestPath };
}

module.exports = { key, selected, resolveBinary };
