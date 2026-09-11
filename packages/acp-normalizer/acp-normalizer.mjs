#!/usr/bin/env node
// acp-normalizer — hydra-acp transformer.
//
// Intercepts response:session/update BEFORE the daemon broadcasts to clients and
// rewrites the per-token fresh `messageId` on agent_message_chunk /
// agent_thought_chunk to ONE stable messageId per logical assistant message.
//
// Why: hydra's `wp()` (recordAndBroadcast) re-injects a messageId into any
// recordable update that lacks one *after* the transform chain, so a
// "strip messageId" transformer could never survive broadcast. Reassigning a
// stable id does survive, because wp only fills in missing ids.
//
// Boundary kinds that start a NEW logical message reset the current id:
//   prompt_received, tool_call, user_message_chunk, agent_message,
//   agent_thought (complete), turn_complete
//
// Deployed declaratively by `lattice.pi-acp-daemon` (module) as a daemon-spawned
// transformer; pinning it in `defaultTransformers` applies it to every session
// without client cooperation. For a quick review it can also be registered at
// runtime via REST /v1/transformers (in-memory only, gone on daemon restart).

const WS_URL = process.env.HYDRA_ACP_WS_URL
const TOKEN = process.env.HYDRA_ACP_TOKEN
const NAME = process.env.HYDRA_ACP_TRANSFORMER_NAME ?? 'acp-normalizer'

if (!WS_URL || !TOKEN) {
  console.error(`[${NAME}] missing HYDRA_ACP_WS_URL / HYDRA_ACP_TOKEN`)
  process.exit(1)
}

const log = (...args) => console.error(`[${NAME}]`, ...args)

// Kinds that begin a fresh logical assistant message.
const BOUNDARY = new Set([
  'prompt_received',
  'tool_call',
  'user_message_chunk',
  'agent_message',
  'agent_thought', // complete thought = a new own message; text after it is new too
  'turn_complete'
])
// Streamed pieces of ONE logical assistant reply.
const CHUNK = new Set(['agent_message_chunk', 'agent_thought_chunk'])

// sessionId -> current logical message id
const currentId = new Map()

function normalize(envelope) {
  const update = envelope?.update
  if (!update || typeof update !== 'object' || Array.isArray(update)) return null
  const kind = update.sessionUpdate
  const sid = envelope.sessionId ?? '(none)'

  if (BOUNDARY.has(kind)) {
    currentId.delete(sid)
    return null
  }
  if (!CHUNK.has(kind)) return null

  const before = update.messageId ?? null
  let id = currentId.get(sid)
  if (id === undefined) {
    id = before ?? crypto.randomUUID()
    currentId.set(sid, id)
  }
  if (before === id) return null
  return { ...envelope, update: { ...update, messageId: id } }
}

function connect(attempt = 0) {
  const ws = new WebSocket(WS_URL, ['acp.v1', `hydra-acp-token.${TOKEN}`])
  const pending = new Map()
  let next = 0
  let connected = false

  const send = (method, params, isRequest = true) =>
    new Promise((resolve, reject) => {
      if (!isRequest) { ws.send(JSON.stringify({ jsonrpc: '2.0', method, params })); resolve(); return }
      const id = ++next
      pending.set(id, { resolve, reject })
      ws.send(JSON.stringify({ jsonrpc: '2.0', id, method, params }))
    })

  ws.addEventListener('message', e => {
    let msg
    try { msg = JSON.parse(e.data) } catch { return }
    if (msg.id !== undefined && pending.has(msg.id)) {
      const p = pending.get(msg.id)
      pending.delete(msg.id)
      if (msg.error) p.reject(new Error(JSON.stringify(msg.error)))
      else p.resolve(msg.result)
      return
    }
    if (msg.method !== 'hydra-acp/transformer/message' || msg.id === undefined) return

    const p = msg.params ?? {}
    if (!p.envelope || typeof p.envelope !== 'object') {
      ws.send(JSON.stringify({ jsonrpc: '2.0', id: msg.id, result: { action: 'continue' } }))
      return
    }
    const payload = normalize(p.envelope)
    if (payload) {
      const u = payload.update
      log(`rewrite session=${(p.sessionId ?? '').slice(0, 12)} phase=${p.phase} kind=${u.sessionUpdate} messageId=${u.messageId}`)
      ws.send(JSON.stringify({ jsonrpc: '2.0', id: msg.id, result: { action: 'continue', payload } }))
    } else {
      ws.send(JSON.stringify({ jsonrpc: '2.0', id: msg.id, result: { action: 'continue' } }))
    }
  })

  ws.addEventListener('open', async () => {
    try {
      log(`connected to ${WS_URL} as transformer "${NAME}"`)
      await send('initialize', { protocolVersion: 1, clientCapabilities: {} })
      const ack = await send('hydra-acp/transformer/initialize', {
        intercepts: ['response:session/update']
      })
      log('transformer/initialize ack:', JSON.stringify(ack))
      connected = true
    } catch (err) {
      log('handshake failed:', err.message)
      ws.close()
    }
  })

  ws.addEventListener('close', () => {
    if (connected || attempt < 10) {
      const delay = Math.min(1000 * 2 ** attempt, 15000)
      log(`closed; reconnect in ${delay}ms (attempt ${attempt + 1})`)
      setTimeout(() => connect(attempt + 1), delay)
    }
  })

  ws.addEventListener('error', () => {})
}

log(`starting (ws=${WS_URL}, name=${NAME})`)
connect()
