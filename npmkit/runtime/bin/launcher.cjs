#!/usr/bin/env node
'use strict';

require('../lib/main.cjs').main().then((code) => {
  process.exitCode = code;
});
