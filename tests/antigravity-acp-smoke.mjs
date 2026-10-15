import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import readline from 'node:readline';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';

const [command] = process.argv.slice(2);
assert.ok(command, 'provide antigravity-acp-fsless executable');

const state = await mkdtemp(path.join(tmpdir(), 'agy-acp-smoke-'));
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
    waiter.reject(new Error(`antigravity-acp-fsless exited ${code}: ${stderr}`));
  }
});

const timeout = setTimeout(() => {
  child.kill('SIGKILL');
}, 30_000);

try {
  // 1. Initialize
  const initRes = await request('initialize', {
    protocolVersion: 1,
    clientCapabilities: {},
  });
  assert.equal(initRes.agentInfo?.name, 'antigravity-acp');

  // 2. Session creation (simulate plain agentrun-openai request without _meta)
  const sessionRes = await request('session/new', {
    cwd: state,
    mcpServers: [],
  });
  assert.ok(sessionRes.sessionId, 'sessionId must be returned');

  // 3. Verify sidecar metadata has tool_filter.enabledTools = []
  const metaPath = path.join(state, 'antigravity-acp', 'conversations', `${sessionRes.sessionId}.meta`);
  const meta = JSON.parse(await readFile(metaPath, 'utf8'));
  assert.deepEqual(meta.tool_filter, { enabledTools: [] });

  console.log('antigravity-acp-fsless smoke test passed: tool_filter correctly injected');
} finally {
  clearTimeout(timeout);
  child.kill('SIGKILL');
  await rm(state, { recursive: true, force: true });
}
