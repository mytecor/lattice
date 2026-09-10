import readline from 'node:readline'

const sessions = new Set()
const activePrompts = new Set()
let nextSession = 0

function send(message) {
  process.stdout.write(`${JSON.stringify(message)}\n`)
}

function result(id, value) {
  send({ jsonrpc: '2.0', id, result: value })
}

function error(id, code, message) {
  send({ jsonrpc: '2.0', id, error: { code, message } })
}

const input = readline.createInterface({ input: process.stdin })

input.on('line', line => {
  if (!line.trim()) return

  const message = JSON.parse(line)
  const { id, method, params = {} } = message

  if (method === 'initialize') {
    result(id, {
      protocolVersion: 1,
      agentInfo: { name: 'lattice-fake-acp', version: '1.0.0' },
      agentCapabilities: {
        loadSession: false,
        mcpCapabilities: { http: false, sse: false },
        promptCapabilities: { image: false, audio: false, embeddedContext: false },
        sessionCapabilities: { list: {} }
      },
      authMethods: []
    })
    return
  }

  if (method === 'session/new') {
    const sessionId = `fake-session-${++nextSession}`
    sessions.add(sessionId)
    result(id, { sessionId })
    return
  }

  if (method === 'session/list') {
    result(id, {
      sessions: [...sessions].map(sessionId => ({
        sessionId,
        cwd: process.cwd(),
        title: sessionId,
        updatedAt: new Date().toISOString()
      }))
    })
    return
  }

  if (method === 'session/prompt') {
    if (!sessions.has(params.sessionId)) {
      error(id, -32602, 'unknown session')
      return
    }
    if (activePrompts.has(params.sessionId)) {
      error(id, -32001, 'overlapping prompt reached the agent')
      return
    }

    activePrompts.add(params.sessionId)
    setTimeout(() => {
      send({
        jsonrpc: '2.0',
        method: 'session/update',
        params: {
          sessionId: params.sessionId,
          update: {
            sessionUpdate: 'agent_message_chunk',
            content: { type: 'text', text: `reply-${id}` }
          }
        }
      })
      activePrompts.delete(params.sessionId)
      result(id, { stopReason: 'end_turn' })
    }, 75)
    return
  }

  if (id !== undefined) error(id, -32601, `unsupported method: ${method}`)
})
