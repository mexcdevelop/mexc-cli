'use strict';

const fs = require('node:fs');
const path = require('node:path');
const childProcess = require('node:child_process');
const semver = require('./semver.cjs');
const manager = require('./manager.cjs');
const platform = require('./platform.cjs');
const { validation, network, internal } = require('./errors.cjs');

function parseFlags(args) {
  const allowed = new Set(['--check', '--force']);
  for (const value of args) {
    if (!allowed.has(value)) {
      throw validation('invalid_argument', `unknown update argument ${value}`, 'use only `--check` or `--force`', value);
    }
  }
  const flags = { check: args.includes('--check'), force: args.includes('--force') };
  if (flags.check && flags.force) {
    throw validation('invalid_argument', '--check and --force cannot be used together', 'choose a read-only check or a forced same-version reinstall');
  }
  return flags;
}

function writeJSON(stream, value) {
  stream.write(`${JSON.stringify(value)}\n`);
}

function verifyVersionOutput(output, expected) {
  const match = /^\S+\s+version\s+(\S+)/m.exec(String(output || '').trim());
  return Boolean(match && match[1] === expected);
}

function guidance(installation, config, target, registry) {
  if (installation.mode === 'npx') {
    return `run \`${manager.exactNPXCommand(config.packageName, target, config.commandName, registry)}\``;
  }
  return `run \`${manager.exactInstallCommand(installation.manager, config.packageName, target, registry, installation.mode === 'local')}\``;
}

function runUpdate(config, args, mainRoot, options = {}) {
  const stdout = options.stdout || process.stdout;
  const runner = options.runner || manager.run;
  const flags = parseFlags(args);
  const current = semver.parse(config.version);
  if (!current) {
    throw validation('failed_precondition', `build version ${config.version} cannot use npm update`, 'install a tagged release build');
  }
  const channel = semver.channel(config.version);
  const installation = manager.detectInstallation(mainRoot, config.packageName, options.env || process.env, runner);
  // The registry that ANSWERED, which is normally the user's own configuration
  // rather than the one stamped in at publish time. Everything downstream — the
  // install, the rollback command, the guidance for a mode we cannot mutate —
  // uses this one, so what was read and what gets installed cannot diverge.
  const found = manager.queryVersion(installation.manager, config.packageName, channel, runner);
  const registry = found.registry;
  const announced = found.version;
  const parsed = semver.parse(announced);
  if (!parsed) {
    throw network(`registry returned invalid Semantic Version ${announced}`, 'retry after the package metadata is corrected');
  }
  // The NORMALISED string from here down, never the one the registry sent.
  // semver.parse validates value.trim() but parseViewVersion returns the JSON
  // string untrimmed, so validating one and installing the other left a gap: a
  // registry answering " 1.2.3\n" passed the check and then reached a command
  // line. Whitespace is all that can survive the regex today, which is why this
  // was a malformed install rather than something worse — and exactly why the
  // two must not be allowed to drift apart again.
  const latest = parsed.raw;
  const ordering = semver.compare(latest, config.version);
  if (flags.check) {
    writeJSON(stdout, {
      package: config.packageName,
      channel,
      current: config.version,
      latest,
      update_available: ordering > 0,
    });
    return 0;
  }
  if (ordering < 0) {
    throw validation('failed_precondition', `refusing to downgrade ${config.packageName} from ${config.version} to ${latest}`, 'correct the registry dist-tag before retrying');
  }
  if (ordering > 0 && flags.force) {
    throw validation('failed_precondition', '--force is only valid when the selected dist-tag matches the installed version', 'omit --force to install the newer version');
  }
  if (ordering === 0 && !flags.force) {
    writeJSON(stdout, { package: config.packageName, current: config.version, latest, updated: false });
    return 0;
  }
  if (installation.mode !== 'global') {
    throw validation(
      'failed_precondition',
      `cannot automatically update installation mode ${installation.mode}`,
      guidance(installation, config, latest, registry),
    );
  }
  const rollback = manager.exactInstallCommand(installation.manager, config.packageName, config.version, registry);
  const installed = manager.installExact(installation.manager, config.packageName, latest, registry, runner);
  if (installed.error || installed.status !== 0) {
    throw network(`package manager failed to install ${config.packageName}@${latest}`, `restore the previous version with \`${rollback}\``);
  }
  const rootResult = runner(installation.manager, ['root', '--global'], { timeout: 5_000 });
  if (rootResult.error || rootResult.status !== 0) {
    throw internal('cannot locate the package manager global root after update', `restore the previous version with \`${rollback}\``);
  }
  const newMainRoot = manager.packagePath(String(rootResult.stdout).trim(), config.packageName);
  let nextConfig;
  try {
    nextConfig = JSON.parse(fs.readFileSync(path.join(newMainRoot, 'lib', 'distribution.json'), 'utf8'));
  } catch (error) {
    throw internal(`cannot read the updated distribution metadata: ${error.message}`, `restore the previous version with \`${rollback}\``);
  }
  if (nextConfig.packageName !== config.packageName || nextConfig.version !== latest) {
    throw validation('failed_precondition', 'updated main package metadata does not match the requested version', `restore the previous version with \`${rollback}\``);
  }
  const resolved = platform.resolveBinary(nextConfig, newMainRoot);
  const verifyRunner = options.verifyRunner || childProcess.spawnSync;
  const verified = verifyRunner(resolved.binary, ['--version'], { encoding: 'utf8', windowsHide: true, timeout: 10_000 });
  if (verified.error || verified.status !== 0 || !verifyVersionOutput(verified.stdout, latest)) {
    throw validation('failed_precondition', `updated binary did not report version ${latest}`, `restore the previous version with \`${rollback}\``);
  }
  writeJSON(stdout, {
    package: config.packageName,
    previous: config.version,
    current: latest,
    updated: true,
    package_manager: installation.manager,
  });
  return 0;
}

module.exports = { parseFlags, verifyVersionOutput, runUpdate };
