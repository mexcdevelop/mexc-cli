'use strict';

const assert = require('node:assert/strict');
const { EventEmitter } = require('node:events');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const update = require('../runtime/lib/update.cjs');
const main = require('../runtime/lib/main.cjs');

function stream() {
  return { text: '', write(value) { this.text += String(value); } };
}

function baseConfig(version = '1.2.3') {
  return {
    packageName: '@example/cli', commandName: 'example', version,
    registry: 'https://registry.example.invalid/', platforms: {},
  };
}

function viewRunner(latest) {
  return (_command, args) => args[0] === 'view'
    ? { status: 0, stdout: `"${latest}"\n` }
    : { status: 1, stderr: 'not global' };
}

test('update check selects latest for releases and beta for prereleases', () => {
  for (const [version, expectedChannel] of [['1.2.3', 'latest'], ['1.3.0-beta.1', 'beta']]) {
    const stdout = stream();
    const calls = [];
    const runner = (command, args, options) => {
      calls.push([command, args, options]);
      return args[0] === 'view' ? { status: 0, stdout: '"1.4.0"\n' } : { status: 1 };
    };
    assert.equal(update.runUpdate(baseConfig(version), ['--check'], '/unknown', { stdout, stderr: stream(), runner }), 0);
    assert.equal(JSON.parse(stdout.text).channel, expectedChannel);
    assert.ok(calls.some(([, args]) => args[1] === `@example/cli@${expectedChannel}`));
  }
});

test('update refuses downgrade and only force reinstalls the selected same version', () => {
  assert.throws(
    () => update.runUpdate(baseConfig(), [], '/unknown', { stdout: stream(), stderr: stream(), runner: viewRunner('1.2.2') }),
    /refusing to downgrade/,
  );
  const stdout = stream();
  assert.equal(update.runUpdate(baseConfig(), [], '/unknown', { stdout, stderr: stream(), runner: viewRunner('1.2.3') }), 0);
  assert.equal(JSON.parse(stdout.text).updated, false);
  assert.throws(
    () => update.runUpdate(baseConfig(), ['--force'], '/unknown', { stdout: stream(), stderr: stream(), runner: viewRunner('1.2.3') }),
    /cannot automatically update installation mode unknown/,
  );
  assert.throws(
    () => update.runUpdate(baseConfig(), ['--force'], '/unknown', { stdout: stream(), stderr: stream(), runner: viewRunner('1.3.0') }),
    /--force is only valid when the selected dist-tag matches/,
  );
  assert.throws(
    () => update.runUpdate(baseConfig(), ['--check', '--force'], '/unknown', { stdout: stream(), stderr: stream(), runner: viewRunner('1.3.0') }),
    /--check and --force cannot be used together/,
  );
});

test('local and npx installs receive precise non-mutating guidance', () => {
  const local = path.join(os.tmpdir(), 'project', 'node_modules', '@example', 'cli');
  try {
    update.runUpdate(baseConfig(), [], local, { stdout: stream(), stderr: stream(), runner: viewRunner('1.3.0') });
    assert.fail('local update should fail');
  } catch (error) {
    assert.match(error.hint, /--save-exact @example\/cli@1\.3\.0/);
  }
  try {
    update.runUpdate(baseConfig(), [], path.join(os.tmpdir(), '_npx', '123', 'node_modules', '@example', 'cli'), {
      stdout: stream(), stderr: stream(), runner: viewRunner('1.3.0'), env: { npm_command: 'exec' },
    });
    assert.fail('npx update should fail');
  } catch (error) {
    assert.match(error.hint, /npx --yes --package @example\/cli@1\.3\.0 -- example --version/);
    // The user's own configuration answered the query, so the command handed back
    // must not override it — that override is the bug this behaviour replaced.
    assert.doesNotMatch(error.hint, /--registry/);
  }
});

test('global npm update installs an exact version and verifies the new platform binary', (t) => {
  const globalRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-update-'));
  t.after(() => fs.rmSync(globalRoot, { recursive: true, force: true }));
  const mainRoot = path.join(globalRoot, '@example', 'cli');
  fs.mkdirSync(mainRoot, { recursive: true });
  fs.writeFileSync(path.join(mainRoot, 'package.json'), '{"name":"@example/cli","version":"1.2.3"}\n');
  const targetKey = `${process.platform}-${process.arch}`;
  const platformName = `@example/cli-${targetKey}`;
  const config = { ...baseConfig(), platforms: { [targetKey]: { packageName: platformName, binary: 'bin/example' } } };
  const calls = [];
  const runner = (command, args) => {
    calls.push([command, args]);
    if (args.join(' ') === 'root --global') return { status: command === 'npm' ? 0 : 1, stdout: `${globalRoot}\n` };
    if (args[0] === 'view') return { status: 0, stdout: '"1.3.0"\n' };
    if (args[0] === 'install') {
      const next = { ...config, version: '1.3.0' };
      fs.mkdirSync(path.join(mainRoot, 'lib'), { recursive: true });
      fs.writeFileSync(path.join(mainRoot, 'lib', 'distribution.json'), JSON.stringify(next));
      const platformRoot = path.join(mainRoot, 'node_modules', ...platformName.split('/'));
      fs.mkdirSync(path.join(platformRoot, 'bin'), { recursive: true });
      fs.writeFileSync(path.join(platformRoot, 'package.json'), JSON.stringify({ name: platformName, version: '1.3.0' }));
      fs.writeFileSync(path.join(platformRoot, 'bin', 'example'), 'binary');
      return { status: 0, stdout: 'installed\n', stderr: '' };
    }
    return { status: 1, stderr: 'unexpected call' };
  };
  const stdout = stream();
  assert.equal(update.runUpdate(config, [], mainRoot, {
    stdout, stderr: stream(), runner,
    verifyRunner: () => ({ status: 0, stdout: 'example version 1.3.0 [public]\n' }),
  }), 0);
  assert.equal(JSON.parse(stdout.text).current, '1.3.0');
  assert.ok(calls.some(([command, args]) => command === 'npm' && args.join(' ').includes('install --global @example/cli@1.3.0')));
});

test('failed post-update verification includes an exact rollback command', (t) => {
  const globalRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-rollback-'));
  t.after(() => fs.rmSync(globalRoot, { recursive: true, force: true }));
  const mainRoot = path.join(globalRoot, '@example', 'cli');
  fs.mkdirSync(mainRoot, { recursive: true });
  fs.writeFileSync(path.join(mainRoot, 'package.json'), '{}');
  const key = `${process.platform}-${process.arch}`;
  const platformName = `@example/cli-${key}`;
  const config = { ...baseConfig(), platforms: { [key]: { packageName: platformName, binary: 'bin/example' } } };
  const runner = (command, args) => {
    if (args.join(' ') === 'root --global') return { status: command === 'npm' ? 0 : 1, stdout: `${globalRoot}\n` };
    if (args[0] === 'view') return { status: 0, stdout: '"1.3.0"' };
    if (args[0] === 'install') {
      fs.mkdirSync(path.join(mainRoot, 'lib'), { recursive: true });
      fs.writeFileSync(path.join(mainRoot, 'lib', 'distribution.json'), JSON.stringify({ ...config, version: '1.3.0' }));
      const platformRoot = path.join(mainRoot, 'node_modules', ...platformName.split('/'));
      fs.mkdirSync(path.join(platformRoot, 'bin'), { recursive: true });
      fs.writeFileSync(path.join(platformRoot, 'package.json'), JSON.stringify({ name: platformName, version: '1.3.0' }));
      fs.writeFileSync(path.join(platformRoot, 'bin', 'example'), 'binary');
      return { status: 0 };
    }
    return { status: 1 };
  };
  assert.throws(
    () => update.runUpdate(config, [], mainRoot, { stdout: stream(), stderr: stream(), runner, verifyRunner: () => ({ status: 1 }) }),
    (error) => /npm install --global @example\/cli@1\.2\.3/.test(error.hint),
  );
});

test('package manager installation failure includes an exact rollback command', (t) => {
  const globalRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'mexc-install-fail-'));
  t.after(() => fs.rmSync(globalRoot, { recursive: true, force: true }));
  const mainRoot = path.join(globalRoot, '@example', 'cli');
  fs.mkdirSync(mainRoot, { recursive: true });
  const runner = (command, args) => {
    if (args.join(' ') === 'root --global') return { status: command === 'npm' ? 0 : 1, stdout: `${globalRoot}\n` };
    if (args[0] === 'view') return { status: 0, stdout: '"1.3.0"' };
    if (args[0] === 'install') return { status: 1, stderr: 'install failed' };
    return { status: 1 };
  };
  assert.throws(
    () => update.runUpdate(baseConfig(), [], mainRoot, { stdout: stream(), stderr: stream(), runner }),
    (error) => /npm install --global @example\/cli@1\.2\.3/.test(error.hint),
  );
});

test('binary launcher forwards argv, environment, stdio, exit code, and signals', async () => {
  let invocation;
  const spawn = (binary, args, options) => {
    invocation = { binary, args, options };
    const child = new EventEmitter();
    child.kill = () => true;
    process.nextTick(() => child.emit('exit', 7, null));
    return child;
  };
  const env = { EXACT: 'value' };
  assert.equal(await main.runBinary('/absolute/example', ['a', '--flag'], env, spawn), 7);
  assert.deepEqual(invocation, {
    binary: '/absolute/example', args: ['a', '--flag'],
    options: { stdio: 'inherit', env, windowsHide: false },
  });
  if (process.platform !== 'win32') {
    let forwarded;
    const signalSpawn = () => {
      const child = new EventEmitter();
      child.kill = () => true;
      process.nextTick(() => child.emit('exit', null, 'SIGTERM'));
      return child;
    };
    assert.equal(await main.runBinary('/absolute/example', [], env, signalSpawn, (signal) => { forwarded = signal; }), 143);
    assert.equal(forwarded, 'SIGTERM');
  }
});

// `spawn UNKNOWN` is the one spawn failure that arrives with no usable message:
// libuv has no mapping for what CreateProcess returns, so the user is handed two
// words. Measured on Windows 11 with Smart App Control enforcing — a freshly
// installed unsigned binary failed exactly that way, the CodeIntegrity log held
// a 3077 naming the file, and a retry succeeded once the verdict was cached.
//
// But the SAME code comes back for a file that is not a valid image at all, which
// is what a download that stopped half way looks like — measured with a text file
// named .exe. The two need opposite advice: retry fixes one, only reinstalling
// fixes the other. So the hint is chosen by reading the image header, and both
// branches are pinned here.
//
// The platform is forced rather than skipped off Windows: the mapping is a pure
// function of the error code and two bytes on disk, and a test that ran on one OS
// only would leave it unverified everywhere it is developed. The header probe is
// exactly that — a header probe — so a file beginning "MZ" is what it treats as
// an image; the test does not pretend to build a real PE.
test('a Windows spawn refusal tells a blocked image apart from a broken one', async () => {
  const realPlatform = process.platform;
  Object.defineProperty(process, 'platform', { value: 'win32', configurable: true });
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'launcher-refusal-'));
  try {
    const image = path.join(dir, 'valid.exe');
    fs.writeFileSync(image, Buffer.concat([Buffer.from('MZ'), Buffer.alloc(64)]));
    const truncated = path.join(dir, 'truncated.exe');
    fs.writeFileSync(truncated, 'this stopped downloading half way');

    const refuse = (code) => () => {
      const child = new EventEmitter();
      child.kill = () => true;
      const error = new Error(`spawn ${code}`);
      error.code = code;
      process.nextTick(() => child.emit('error', error));
      return child;
    };
    const failure = async (binary, code) => {
      let captured;
      await assert.rejects(
        () => main.runBinary(binary, [], {}, refuse(code), undefined, 'example'),
        (error) => { captured = error; return true; },
      );
      assert.match(captured.message, /cannot start platform binary/);
      return captured;
    };

    // A real image the OS refused: policy is the explanation, retry is the action,
    // and the log makes the claim checkable instead of asserted.
    for (const code of ['UNKNOWN', 'EPERM', 'EACCES']) {
      const error = await failure(image, code);
      assert.ok(error.hint, `${code} on a valid image must carry a hint`);
      assert.match(error.hint, /again/);
      assert.match(error.hint, /CodeIntegrity/);
      // The command name comes from the distribution config, never a literal, so
      // another product built on this kit names itself here.
      assert.match(error.hint, /`example doctor`/);
    }

    // Not an image: retrying cannot help, so the hint must not suggest it, and
    // must not blame a policy that did nothing.
    const broken = await failure(truncated, 'UNKNOWN');
    assert.ok(broken.hint, 'a broken image must carry a hint too');
    assert.match(broken.hint, /Reinstall/);
    assert.doesNotMatch(broken.hint, /CodeIntegrity/);
    assert.doesNotMatch(broken.hint, /again first/);

    // A missing file is already self-explanatory — its message carries the path —
    // so it gets no hint at all.
    const missing = await failure(path.join(dir, 'absent.exe'), 'ENOENT');
    assert.equal(missing.hint, undefined);
  } finally {
    Object.defineProperty(process, 'platform', { value: realPlatform, configurable: true });
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('launcher failures use the JSON error envelope on stderr only', async () => {
  const stdout = stream();
  const stderr = { ...stream(), isTTY: false };
  const code = await main.main([], {
    config: { ...baseConfig(), commandName: 'example', stateDir: '.example', refreshMs: 1000 },
    mainRoot: '/absolute/main',
    platform: 'win32',
    arch: 'arm64',
    stdout,
    stderr,
  });
  assert.equal(code, 2);
  assert.equal(stdout.text, '');
  const envelope = JSON.parse(stderr.text);
  assert.equal(envelope.error.type, 'validation');
  assert.equal(envelope.error.subtype, 'failed_precondition');
});
