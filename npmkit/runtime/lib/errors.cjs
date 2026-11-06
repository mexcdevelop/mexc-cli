'use strict';

const EXIT_CODES = {
  validation: 2,
  network: 4,
  internal: 5,
  api: 6,
};

class CLIError extends Error {
  constructor(type, subtype, message, options = {}) {
    super(message);
    this.name = 'CLIError';
    this.type = type;
    this.subtype = subtype;
    this.param = options.param;
    this.hint = options.hint;
    this.exitCode = EXIT_CODES[type] || EXIT_CODES.internal;
  }
}

function validation(subtype, message, hint, param) {
  return new CLIError('validation', subtype, message, { hint, param });
}

function network(message, hint) {
  return new CLIError('network', 'network_transport', message, { hint });
}

function internal(message, hint) {
  return new CLIError('internal', 'failed_precondition', message, { hint });
}

function normalize(error) {
  if (error instanceof CLIError) return error;
  return internal(error && error.message ? error.message : String(error));
}

function writeError(error, stream = process.stderr) {
  const value = normalize(error);
  const body = {
    type: value.type,
    subtype: value.subtype,
    message: value.message,
  };
  if (value.param) body.param = value.param;
  if (value.hint) body.hint = value.hint;
  stream.write(`${JSON.stringify({ error: body })}\n`);
  return value.exitCode;
}

module.exports = { CLIError, validation, network, internal, normalize, writeError };
