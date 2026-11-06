'use strict';

const fs = require('node:fs');
const path = require('node:path');
const childProcess = require('node:child_process');
const cache = require('./cache.cjs');
const platform = require('./platform.cjs');
const update = require('./update.cjs');
const { internal, writeError } = require('./errors.cjs');

const SIGNAL_EXIT_CODES = { SIGHUP: 129, SIGINT: 130, SIGTERM: 143 };

// The error codes Windows returns when something refuses to let an image start,
// rather than when the image is missing or broken.
//
// UNKNOWN is the one that matters. A code-integrity policy — Smart App Control,
// or an enterprise WDAC policy — fails CreateProcess with a status libuv has no
// mapping for, so it reports its own fallback and the real cause appears nowhere
// in the message. EPERM and EACCES arrive from the same class of refusal, and
// from antivirus holding a file it is still scanning.
const WINDOWS_REFUSAL_CODES = new Set(['UNKNOWN', 'EPERM', 'EACCES']);

// looksLikeWindowsImage reads the two bytes every PE image begins with.
//
// This exists because `spawn UNKNOWN` cannot, on its own, tell a policy refusal
// from a broken file. Measured: a text file named .exe fails with exactly the
// same UNKNOWN a code-integrity block produces, since CreateProcess rejects both
// with a status libuv has no mapping for. The two need opposite advice — one is
// fixed by running the command again, the other only by reinstalling — so
// guessing would send half the readers the wrong way.
//
// Execution and reading are separate permissions, so a file a policy refuses to
// START still reads normally and still has its MZ header. A file that lacks it
// was not written completely.
function looksLikeWindowsImage(binary) {
  try {
    const handle = fs.openSync(binary, 'r');
    try {
      const head = Buffer.alloc(2);
      return fs.readSync(handle, head, 0, 2, 0) === 2 && head[0] === 0x4d && head[1] === 0x5a;
    } finally {
      fs.closeSync(handle);
    }
  } catch (_) {
    // Unreadable is not evidence of a valid image, and the caller's fallback for
    // "not an image" is the safer of the two messages.
    return false;
  }
}

// startFailure turns a spawn error into the error the user reads.
//
// Without this, a policy decision reaches the user as `cannot start platform
// binary: spawn UNKNOWN`: five words that name no cause, suggest a corrupt
// install, and give nothing to act on. Measured on Windows 11 with Smart App
// Control enforcing — a freshly installed unsigned binary failed exactly that
// way, the CodeIntegrity operational log held a 3077 naming the file, and the
// same command succeeded a minute later once the verdict was cached. Retrying is
// therefore the first thing to try, and it is what the hint says first.
//
// Other platforms and other codes keep the plain message. A hint that guessed at
// Windows policy on a Linux ENOENT would be worse than none.
function startFailure(error, binary, commandName) {
  const detail = error && error.message ? error.message : String(error);
  if (process.platform !== 'win32' || !WINDOWS_REFUSAL_CODES.has(error && error.code)) {
    return internal(`cannot start platform binary: ${detail}`);
  }
  if (!looksLikeWindowsImage(binary)) {
    return internal(`cannot start platform binary: ${detail}`,
      'the file is not a valid Windows executable, which is what a download that did not '
      + 'finish writing looks like. Reinstall the package — running the command again will '
      + 'not change this.');
  }
  const doctor = commandName ? '`' + commandName + " doctor`" : 'the doctor command';
  return internal(`cannot start platform binary: ${detail}`,
    'Windows refused to start it. That is what a code-integrity policy does — Smart App '
    + 'Control blocks an unsigned binary while it evaluates one, and antivirus can hold a '
    + 'file that was just installed — so this is usually not a broken install. Run the same '
    + 'command again first: the verdict is cached, and a second attempt usually succeeds. If '
    + 'it keeps failing, look for a 3077 or 3118 event naming this file in '
    + '`Get-WinEvent -LogName Microsoft-Windows-CodeIntegrity/Operational -MaxEvents 20`, '
    + 'and see ' + doctor + '.');
}

function loadConfig() {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'distribution.json'), 'utf8'));
}

function runBinary(binary, args, env = process.env, spawn = childProcess.spawn, signalSelf = (signal) => process.kill(process.pid, signal), commandName = '') {
  return new Promise((resolve, reject) => {
    let child;
    try {
      child = spawn(binary, args, { stdio: 'inherit', env, windowsHide: false });
    } catch (error) {
      reject(startFailure(error, binary, commandName));
      return;
    }
    const signals = process.platform === 'win32' ? ['SIGINT', 'SIGTERM', 'SIGBREAK'] : ['SIGINT', 'SIGTERM', 'SIGHUP'];
    const handlers = new Map();
    for (const signal of signals) {
      const handler = () => {
        try { child.kill(signal); } catch (_) { /* Child may already have exited. */ }
      };
      handlers.set(signal, handler);
      process.on(signal, handler);
    }
    const cleanup = () => {
      for (const [signal, handler] of handlers) process.removeListener(signal, handler);
    };
    child.once('error', (error) => {
      cleanup();
      reject(startFailure(error, binary, commandName));
    });
    child.once('exit', (code, signal) => {
      cleanup();
      if (typeof code === 'number') {
        resolve(code);
        return;
      }
      if (signal && process.platform !== 'win32') {
        try {
          signalSelf(signal);
        } catch (_) {
          resolve(1);
          return;
        }
        resolve(SIGNAL_EXIT_CODES[signal] || 1);
        return;
      }
      resolve(1);
    });
  });
}

async function main(args = process.argv.slice(2), options = {}) {
  try {
    const config = options.config || loadConfig();
    const mainRoot = options.mainRoot || path.resolve(__dirname, '..');
    if (args[0] === 'update') {
      return update.runUpdate(config, args.slice(1), mainRoot, options);
    }
    cache.maybeNotify(config, args, options);
    const resolved = platform.resolveBinary(config, mainRoot, options.platform, options.arch);
    return await runBinary(resolved.binary, args, options.env || process.env, options.spawn, options.signalSelf, config.commandName);
  } catch (error) {
    return writeError(error, options.stderr || process.stderr);
  }
}

module.exports = { loadConfig, runBinary, main };
