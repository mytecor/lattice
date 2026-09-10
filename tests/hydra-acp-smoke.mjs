import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, mkdir, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'

const [daemonCommand, nodeCommand, fakeAgent] = process.argv.slice(2)
if (!daemonCommand || !nodeCommand || !fakeAgent) {
  throw new Error('usage: hydra-acp-smoke.mjs <hydra-daemon> <node> <fake-agent>')
}

const hydraHome = await mkdtemp(path.join(tmpdir(), 'lattice-hydra-smoke-'))
const token = 'hydra-smoke-service-token'
const port = 45614

await mkdir(path.join(hydraHome, 'xdg'), { recursive: true })
await writeFile(path.join(hydraHome, 'auth-token'), `${token}\n`, { mode: 0o600 })
await writeFile(
  path.join(hydraHome, 'config.json'),
  JSON.stringify({
    daemon: {
      host: '127.0.0.1',
      port,
      logLevel: 'error',
      sessionIdleTimeoutSeconds: 0,
      nonInteractiveOrphanTimeoutSeconds: 0
    },
    registry: { pinned: true },
    agents: {
      'fake-acp': {
        command: nodeCommand,
        args: [fakeAgent]
      }
    },
    defaultAgent: 'fake-acp',
    defaultCwd: hydraHome
  }),
  { mode: 0o600 }
)

const daemon = spawn(daemonCommand, [], {
  env: {
    ...process.env,
    HYDRA_ACP_HOME: hydraHome,
    XDG_CONFIG_HOME: path.join(hydraHome, 'xdg')
  },
  stdio: ['ignore', 'ignore', 'inherit']
})
const daemonExited = new Promise(resolve => daemon.once('exit', resolve))

let nextId = 0

async function connect() {
  let lastError
  for (let attempt = 0; attempt < 100; attempt += 1) {
    try {
      const socket = new WebSocket(`ws://127.0.0.1:${port}/acp`, [
        'acp.v1',
        `hydra-acp-token.${token}`
      ])
      await new Promise((resolve, reject) => {
        socket.addEventListener('open', resolve, { once: true })
        socket.addEventListener('error', reject, { once: true })
      })

      const pending = new Map()
      const updates = []
      socket.addEventListener('message', event => {
        const message = JSON.parse(event.data)
        if (message.method === 'session/update') updates.push(message.params)
        if (message.id === undefined) return
        const request = pending.get(message.id)
        if (!request) return
        pending.delete(message.id)
        if (message.error) request.reject(new Error(JSON.stringify(message.error)))
        else request.resolve(message.result)
      })

      return {
        socket,
        updates,
        request(method, params) {
          const id = ++nextId
          return new Promise((resolve, reject) => {
            pending.set(id, { resolve, reject })
            socket.send(JSON.stringify({ jsonrpc: '2.0', id, method, params }))
          })
        }
      }
    } catch (error) {
      lastError = error
      await new Promise(resolve => setTimeout(resolve, 50))
    }
  }
  throw lastError ?? new Error('Hydra did not accept WebSocket connections')
}

async function initialize(client) {
  const response = await client.request('initialize', {
    protocolVersion: 1,
    clientCapabilities: {}
  })
  assert.equal(response.protocolVersion, 1)
  assert.ok(response.agentCapabilities.sessionCapabilities.attach)
  assert.ok(response.agentCapabilities.sessionCapabilities.list)
}

async function close(client) {
  client.socket.close()
  await new Promise(resolve => client.socket.addEventListener('close', resolve, { once: true }))
}

async function waitForUpdates(client, count) {
  for (let attempt = 0; attempt < 100 && client.updates.length < count; attempt += 1) {
    await new Promise(resolve => setTimeout(resolve, 20))
  }
  assert.ok(client.updates.length >= count, `expected ${count} updates, got ${client.updates.length}`)
}

try {
  const first = await connect()
  await initialize(first)

  const sessionA = await first.request('session/new', { cwd: hydraHome, mcpServers: [] })
  const sessionB = await first.request('session/new', { cwd: hydraHome, mcpServers: [] })
  assert.notEqual(sessionA.sessionId, sessionB.sessionId)
  const secondSessionPrompt = await first.request('session/prompt', {
    sessionId: sessionB.sessionId,
    prompt: [{ type: 'text', text: 'parallel session' }]
  })
  assert.equal(secondSessionPrompt.stopReason, 'end_turn')
  first.updates.length = 0

  const second = await connect()
  await initialize(second)
  await second.request('session/attach', {
    sessionId: sessionA.sessionId,
    historyPolicy: 'none'
  })

  const firstPrompt = first.request('session/prompt', {
    sessionId: sessionA.sessionId,
    prompt: [{ type: 'text', text: 'first' }]
  })
  const secondPrompt = second.request('session/prompt', {
    sessionId: sessionA.sessionId,
    prompt: [{ type: 'text', text: 'second' }]
  })
  const promptResults = await Promise.all([firstPrompt, secondPrompt])
  assert.deepEqual(promptResults.map(result => result.stopReason), ['end_turn', 'end_turn'])
  await waitForUpdates(first, 2)
  await waitForUpdates(second, 2)

  await close(first)
  await close(second)

  const reconnected = await connect()
  await initialize(reconnected)
  const listed = await reconnected.request('session/list', { cwd: hydraHome })
  const ids = listed.sessions.map(session => session.sessionId)
  assert.ok(ids.includes(sessionA.sessionId), ids)
  assert.ok(ids.includes(sessionB.sessionId), ids)

  await reconnected.request('session/attach', {
    sessionId: sessionA.sessionId,
    historyPolicy: 'full'
  })
  const resumed = await reconnected.request('session/prompt', {
    sessionId: sessionA.sessionId,
    prompt: [{ type: 'text', text: 'after reconnect' }]
  })
  assert.equal(resumed.stopReason, 'end_turn')
  await close(reconnected)

  const metadata = JSON.parse(
    await readFile(path.join(hydraHome, 'sessions', sessionA.sessionId, 'meta.json'), 'utf8')
  )
  assert.equal(metadata.sessionId, sessionA.sessionId)
} finally {
  if (daemon.exitCode === null) daemon.kill('SIGTERM')
  await daemonExited
}

console.log('Hydra ACP smoke passed: multi-session, multi-client queue/broadcast, reconnect')
