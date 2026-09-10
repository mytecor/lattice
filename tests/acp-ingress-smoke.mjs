// LAN ingress acceptance: exercises the exact external ACP WebSocket shape that
// Ferngeist-style clients use through the Caddy host
// ws://acp.<nodename>.local/ (rewritten internally to /acp?token=...).
//
// Unlike hydra-acp-smoke.mjs (which uses the local daemon URL and the token
// subprotocol), this test connects exactly like an external client:
//   - subprotocol list contains only `acp.v1` (no hydra-acp-token.*);
//   - URL is /acp?token=<public loopback token>, the shape Caddy produces.
//
// It verifies initialize, two parallel sessions, two clients attached to one
// live session, a consistent event stream on both, and serialized prompts.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, mkdir, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'

const [daemonCommand, nodeCommand, fakeAgent] = process.argv.slice(2)
if (!daemonCommand || !nodeCommand || !fakeAgent) {
  throw new Error('usage: acp-ingress-smoke.mjs <hydra-daemon> <node> <fake-agent>')
}

const hydraHome = await mkdtemp(path.join(tmpdir(), 'lattice-acp-ingress-'))
const token = 'caddy-loopback-token'
const port = 45664

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
      'fake-acp': { command: nodeCommand, args: [fakeAgent] }
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
      const socket = new WebSocket(`ws://127.0.0.1:${port}/acp?token=${token}`, ['acp.v1'])
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

async function close(client) {
  client.socket.close()
  await new Promise(resolve =>
    client.socket.addEventListener('close', resolve, { once: true })
  )
}

async function waitForUpdates(client, count) {
  for (let attempt = 0; attempt < 100 && client.updates.length < count; attempt += 1) {
    await new Promise(resolve => setTimeout(resolve, 20))
  }
  assert.ok(
    client.updates.length >= count,
    `expected ${count} updates, got ${client.updates.length}`
  )
}

try {
  const first = await connect()

  const init = await first.request('initialize', { protocolVersion: 1, clientCapabilities: {} })
  assert.equal(init.protocolVersion, 1)
  assert.ok(init.agentCapabilities.sessionCapabilities.attach)
  assert.ok(init.agentCapabilities.sessionCapabilities.list)

  // Two parallel live sessions from one client.
  const sessionA = await first.request('session/new', { cwd: hydraHome, mcpServers: [] })
  const sessionB = await first.request('session/new', { cwd: hydraHome, mcpServers: [] })
  assert.notEqual(sessionA.sessionId, sessionB.sessionId)
  const parallel = await first.request('session/prompt', {
    sessionId: sessionB.sessionId,
    prompt: [{ type: 'text', text: 'parallel session' }]
  })
  assert.equal(parallel.stopReason, 'end_turn')

  // External client never leaks an internal token: negotiated subprotocol is acp.v1.
  assert.equal(first.socket.protocol, 'acp.v1')

  // Second external client attaches to the same live session.
  const second = await connect()
  await second.request('initialize', { protocolVersion: 1, clientCapabilities: {} })
  await second.request('session/attach', {
    sessionId: sessionA.sessionId,
    historyPolicy: 'none'
  })

  // Both clients prompt the shared session; prompts are serialized, both
  // receive the same consistent event stream.
  first.updates.length = 0
  second.updates.length = 0
  const firstPrompt = first.request('session/prompt', {
    sessionId: sessionA.sessionId,
    prompt: [{ type: 'text', text: 'first client' }]
  })
  const secondPrompt = second.request('session/prompt', {
    sessionId: sessionA.sessionId,
    prompt: [{ type: 'text', text: 'second client' }]
  })
  const promptResults = await Promise.all([firstPrompt, secondPrompt])
  assert.deepEqual(
    promptResults.map(result => result.stopReason),
    ['end_turn', 'end_turn']
  )
  await waitForUpdates(first, 2)
  await waitForUpdates(second, 2)
  // Both clients receive the content of every turn (two `reply-` chunks from
  // the shared session), proving a consistent event stream with no cross
  // delivery of one client's responses to the other.
  const replyCount = updates => (JSON.stringify(updates).match(/reply-/g) ?? []).length
  assert.ok(replyCount(first.updates) >= 2, `first client saw ${replyCount(first.updates)} replies`)
  assert.ok(replyCount(second.updates) >= 2, `second client saw ${replyCount(second.updates)} replies`)

  // Disconnect does not kill the session; a fresh client resumes it and can
  // keep prompting.
  await close(first)
  await close(second)

  const reconnected = await connect()
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

  // Daemon restart: session metadata lives in HYDRA_ACP_HOME, so Hydra re-seeds
  // the session index on boot. The sessions come back (cold), and a client can
  // attach and prompt again without a new URL or config.
  daemon.kill('SIGTERM')
  await Promise.race([daemonExited, new Promise(resolve => setTimeout(resolve, 3000))])

  const restarted = spawn(daemonCommand, [], {
    env: {
      ...process.env,
      HYDRA_ACP_HOME: hydraHome,
      XDG_CONFIG_HOME: path.join(hydraHome, 'xdg')
    },
    stdio: ['ignore', 'ignore', 'inherit']
  })
  const restartedExited = new Promise(resolve => restarted.once('exit', resolve))

  const survivor = await connect()
  const afterRestart = await survivor.request('session/list', { cwd: hydraHome })
  const survivorIds = afterRestart.sessions.map(session => session.sessionId)
  assert.ok(survivorIds.includes(sessionA.sessionId), survivorIds)
  assert.ok(survivorIds.includes(sessionB.sessionId), survivorIds)
  await survivor.request('session/attach', {
    sessionId: sessionA.sessionId,
    historyPolicy: 'full'
  })
  const afterRestartPrompt = await survivor.request('session/prompt', {
    sessionId: sessionA.sessionId,
    prompt: [{ type: 'text', text: 'after daemon restart' }]
  })
  assert.equal(afterRestartPrompt.stopReason, 'end_turn')
  await close(survivor)
  await restarted.kill('SIGTERM')
  await Promise.race([restartedExited, new Promise(resolve => setTimeout(resolve, 3000))])

  console.log(
    'ACP ingress smoke passed: acp.v1-only clients, multi-session, multi-client shared stream, reconnect, daemon restart'
  )
} finally {
  daemon.kill('SIGTERM')
  await Promise.race([daemonExited, new Promise(resolve => setTimeout(resolve, 3000))])
}
