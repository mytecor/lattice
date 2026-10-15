import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import readline from 'node:readline';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';

const [command] = process.argv.slice(2);
assert.ok(command, 'provide gemini-acp-fsless executable');

const state = await mkdtemp(path.join(tmpdir(), 'gemini-acp-smoke-'));
const child = spawn(command, [], {
  env: {
    PATH: process.env.PATH,
    GEMINI_HOME: state,
    GEMINI_API_KEY: 'test-smoke-key',
  },
  stdio: ['pipe', 'pipe', 'pipe'],
});

let stderr = '';
child.stderr.on('data', (chunk) => {
  stderr += chunk;
});

const rl = readline.createInterface({ input: child.stdout });
const pending = new Map();
let seq = 0;

rl.on('line', (line) => {
  const trimmed = line.trim();
  if (!trimmed) return;
  const msg = JSON.parse(trimmed);
  const waiter = pending.get(msg.id);
  if (waiter) {
    pending.delete(msg.id);
    if (msg.error) waiter.reject(new Error(JSON.stringify(msg.error)));
    else waiter.resolve(msg.result);
  }
});

function request(method, params) {
  return new Promise((resolve, reject) => {
    const id = ++seq;
    pending.set(id, { resolve, reject });
    child.stdin.write(JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n');
  });
}

child.on('exit', (code) => {
  for (const waiter of pending.values()) {
    waiter.reject(new Error(`gemini-acp-fsless exited ${code}: ${stderr}`));
  }
});

const timeout = setTimeout(() => {
  child.kill('SIGKILL');
}, 30_000);

try {
  // 1. Initialize (simulate client capabilities with fs/terminal that must be stripped by filter)
  const initRes = await request('initialize', {
    protocolVersion: 1,
    clientCapabilities: {
      fs: { readTextFile: true, writeTextFile: true },
      terminal: true,
    },
  });
  assert.equal(initRes.agentInfo?.name, 'gemini-cli');

  // 2. Session creation (simulate plain agentrun-openai request)
  const sessionRes = await request('session/new', {
    cwd: state,
    mcpServers: [],
  });
  assert.ok(sessionRes.sessionId, 'sessionId must be returned');

  // 3. Verify settings.json was seeded with tools.exclude blocking shell & fs tools
  const settingsPath = path.join(state, '.gemini', 'settings.json');
  const settings = JSON.parse(await readFile(settingsPath, 'utf8'));
  assert.ok(Array.isArray(settings.tools?.exclude), 'tools.exclude must be configured');
  assert.ok(settings.tools.exclude.includes('run_shell_command'), 'run_shell_command must be excluded');
  assert.ok(settings.tools.exclude.includes('read_file'), 'read_file must be excluded');
  assert.ok(settings.tools.exclude.includes('write_file'), 'write_file must be excluded');

  // 4. Verify admin policy was linked into place
  const policyPath = path.join(state, '.gemini', 'policies', 'fsless.toml');
  const policyContent = await readFile(policyPath, 'utf8');
  assert.ok(policyContent.includes('run_shell_command'), 'policy must include run_shell_command');
  assert.ok(policyContent.includes('decision = "deny"'), 'policy must deny built-in tools');
  assert.ok(policyContent.includes('mcpName = "*"'), 'policy must allow mcp tools');

  console.log('gemini-acp-fsless smoke test passed: isolation and fsless tool filtering verified');
} finally {
  clearTimeout(timeout);
  child.kill('SIGKILL');
  await new Promise((resolve) => {
    if (child.exitCode !== null || child.signalCode !== null) resolve();
    else child.on('exit', resolve);
  });
  await rm(state, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
}
