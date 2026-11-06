'use strict';

// Host gates for tests whose subject only exists on one kind of machine.
//
// Both of these are REFUSALS the product makes on purpose, not gaps to work
// around, so the tests that need the other kind of host say so and skip rather
// than fail. Each has a counterpart that asserts the refusal itself, so nothing
// here hides a behaviour instead of covering it.

const process = require('node:process');

// requirePosixPackagingHost: npm packaging cannot produce a POSIX tarball on
// Windows.
//
// `npm pack` reads each entry's mode from the filesystem and Windows has no
// executable bit to give it, so a darwin or linux binary ships as 0644 and fails
// to start once installed — well-formed, correctly named, and dead. package.mjs
// refuses up front (assertHostCanPack), which is why a test that expects a
// SUCCESSFUL multi-platform packaging run cannot run here.
//
// Not a blanket "skip packaging tests on Windows": a windows-ONLY matrix
// packages fine on a Windows host and two tests do exactly that. Only the POSIX
// targets are gated.
function requirePosixPackagingHost(t) {
  if (process.platform === 'win32') {
    t.skip('npm packaging refuses POSIX targets on a Windows host (assertHostCanPack); '
      + 'the refusal itself is covered by "npm packaging refuses a POSIX target on a Windows host"');
    return true;
  }
  return false;
}

module.exports = { requirePosixPackagingHost };
