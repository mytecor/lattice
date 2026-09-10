import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'

const [adapterCommand] = process.argv.slice(2)
if (!adapterCommand) throw new Error('usage: pi-acp-smoke.mjs <pi-acp>')

const state = await mkdtemp(path.join(tmpdir(), 'lattice-pi-acp-smoke-'))
const child = spawn(adapterCommand, [], {
  env: {
    ...process.env,
    PI_ACP_DIR: path.join(state, 'adapter'),
    PI_CODING_AGENT_DIR: path.join(state, 'agent'),
    PI_CODING_AGENT_SESSION_DIR: path.join(state, 'sessions'),
    ANTHROPIC_API_KEY: 'pi-acp-smoke-no-call',
    OPENAI_API_KEY: 'pi-acp-smoke-no-call'
  },
  stdio: ['pipe', 'pipe', 'inherit']
})

const pending = new Map()
let nextId = 0
let buffer = ''

child.stdout.setEncoding('utf8').on('data', chunk => {
  buffer += chunk
  let newline
  while ((newline = buffer.indexOf('\n')) >= 0) {
    const line = buffer.slice(0, newline)
    buffer = buffer.slice(newline + 1)
    if (!line.trim()) continue
    const message = JSON.parse(line)
    const request = pending.get(message.id)
    if (!request) continue
    pending.delete(message.id)
    if (message.error) request.reject(new Error(JSON.stringify(message.error)))
    else request.resolve(message.result)
  }
})

function request(method, params) {
  const id = ++nextId
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject })
    child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id, method, params })}\n`)
  })
}

const timeout = setTimeout(() => child.kill('SIGKILL'), 30_000)

try {
  const initialized = await request('initialize', {
    protocolVersion: 1,
    clientCapabilities: {}
  })
  assert.equal(initialized.protocolVersion, 1)
  assert.equal(initialized.agentInfo.name, 'pi-acp')

  const session = await request('session/new', { cwd: state, mcpServers: [] })
  assert.ok(session.sessionId)

  const prompt = await request('session/prompt', {
    sessionId: session.sessionId,
    prompt: [{ type: 'text', text: '/name lattice-pi-acp-smoke' }]
  })
  assert.equal(prompt.stopReason, 'end_turn')
} finally {
  child.stdin.end()
  const exitCode = await new Promise(resolve => child.once('exit', resolve))
  clearTimeout(timeout)
  assert.equal(exitCode, 0)
}

console.log('Pi ACP smoke passed: initialize, session/new, built-in prompt')
