import { spawn } from 'node:child_process'
import { createInterface } from 'node:readline'
import { once } from 'node:events'

// agentrun starts one ACP process per native session. Its model-discovery
// process has no MCP servers. A chat may have the session-scoped function
// bridge, or no tools at all. Muse rejects a wildcard with no matching server,
// so select the host toolset before admitting the first session.
const [adapter, ...args] = process.argv.slice(2)
const bridgeName = 'openai-functions'
let child
let initialize
let selected = false
let sessionAdmitted = false
let replay
let stopping = false

function start(withTools) {
  const toolset = withTools ? '["mcp__openai_functions__*"]' : '[]'
  const current = spawn(adapter, args, {
    env: { ...process.env, MUSE_SERVE_ARGS:
      '--disable-write --disable-shell --disable-request-user-input ' +
      `--config run.toolset=${toolset} --config run.reminder_roster.agents=[]` },
    stdio: ['pipe', 'pipe', 'inherit']
  })
  child = current
  current.stdin.on('error', () => {
    if (current === child && !stopping) fail('adapter input closed')
  })
  const lines = createInterface({ input: current.stdout })
  lines.on('line', line => {
    let message
    try { message = JSON.parse(line) } catch { fail('invalid adapter JSON'); return }
    if (replay && current === child && message.id === initialize.id && !message.method) {
      const pending = replay
      replay = undefined
      if (message.error) pending.reject(new Error('adapter initialization failed'))
      else pending.resolve()
      return
    }
    process.stdout.write(line + '\n')
  })
  current.on('error', () => fail('adapter could not start'))
  current.on('exit', code => {
    if (current === child && !stopping) {
      replay?.reject(new Error('adapter exited during initialization'))
      process.exitCode = code || 1
      process.stdin.destroy()
    }
  })
}

async function stop() {
  if (!child || child.exitCode !== null) return
  const current = child
  const exited = once(current, 'exit')
  child = undefined
  current.stdin.end()
  const timeout = setTimeout(() => current.kill('SIGTERM'), 10_000)
  const deadline = setTimeout(() => current.kill('SIGKILL'), 12_000)
  try { await exited } finally { clearTimeout(timeout); clearTimeout(deadline) }
}

function fail(reason) {
  console.error(`muse-acp-fsless: ${reason}`)
  process.exitCode = 1
  process.stdin.destroy()
}

const input = createInterface({ input: process.stdin })
let queue = Promise.resolve()
input.on('line', line => {
  queue = queue.then(async () => {
    const message = JSON.parse(line)
    if (!child) start(false)
    if (message.method === 'initialize') initialize = message
    if (['session/new', 'session/load', 'session/resume'].includes(message.method)) {
      const servers = message.params?.mcpServers ?? []
      if (!Array.isArray(servers) || servers.some(server => server.name !== bridgeName)) {
        process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: message.id,
          error: { code: -32602, message: 'Only the agentrun openai-functions MCP bridge is supported' } }) + '\n')
        return
      }
      const withTools = servers.length > 0
      if (sessionAdmitted && selected !== withTools) {
        process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: message.id,
          error: { code: -32602, message: 'Start a fresh ACP process when changing the MCP toolset' } }) + '\n')
        return
      }
      if (selected !== withTools) {
        if (!initialize) throw new Error('initialize must precede session setup')
        await stop()
        start(withTools)
        const initialized = new Promise((resolve, reject) => { replay = { resolve, reject } })
        const timeout = setTimeout(() => replay?.reject(new Error('initialization timed out')), 30_000)
        child.stdin.write(JSON.stringify(initialize) + '\n')
        try { await initialized } finally { clearTimeout(timeout); replay = undefined }
        child.stdin.write('{"jsonrpc":"2.0","method":"initialized","params":{}}\n')
        selected = withTools
      }
      sessionAdmitted = true
    }
    child.stdin.write(line + '\n')
  }).catch(() => fail('protocol forwarding failed'))
})
input.on('close', async () => {
  stopping = true
  await queue
  await stop()
})
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => { stopping = true; input.close(); process.stdin.destroy() })
}
