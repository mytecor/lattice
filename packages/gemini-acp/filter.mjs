#!/usr/bin/env node

import { spawn } from 'node:child_process';
import readline from 'node:readline';

const serverBin = process.argv[2];
const serverArgs = process.argv.slice(3);

if (!serverBin) {
  console.error('Usage: filter.mjs <server-binary> [args...]');
  process.exit(1);
}

const child = spawn(serverBin, serverArgs, {
  stdio: ['pipe', 'pipe', 'inherit'],
  env: process.env,
});

child.on('error', (err) => {
  console.error(`gemini-acp-fsless: failed to spawn ${serverBin}:`, err);
  process.exit(1);
});

child.stdout.pipe(process.stdout);

const rl = readline.createInterface({
  input: process.stdin,
  crlfDelay: Infinity,
});

rl.on('line', (line) => {
  const trimmed = line.trim();
  if (!trimmed) {
    child.stdin.write(line + '\n');
    return;
  }
  try {
    const msg = JSON.parse(trimmed);
    if (msg && msg.method === 'initialize') {
      if (msg.params?.clientCapabilities) {
        delete msg.params.clientCapabilities.fs;
        delete msg.params.clientCapabilities.terminal;
      }
      child.stdin.write(JSON.stringify(msg) + '\n');
      return;
    }
  } catch (_) {
    // Non-JSON or malformed line; forward as-is.
  }
  child.stdin.write(line + '\n');
});

rl.on('close', () => {
  child.stdin.end();
});

child.on('exit', (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
  } else {
    process.exit(code ?? 0);
  }
});
