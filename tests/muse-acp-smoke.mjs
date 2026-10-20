import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, mkdir, writeFile, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import http from 'node:http'
import { gunzipSync } from 'node:zlib'

const [command, mode = 'tools'] = process.argv.slice(2)
assert.ok(command, 'provide muse-acp-fsless executable')
const state = await mkdtemp(path.join(tmpdir(), 'muse-fsless-'))
await mkdir(path.join(state, 'muse/config/muse'), { recursive: true })
const requests = []
const server = http.createServer(async (req, res) => {
  const chunks = []
  for await (const chunk of req) chunks.push(chunk)
  let body = Buffer.concat(chunks)
  if (req.headers['content-encoding'] === 'gzip') body = gunzipSync(body)
  body = body.toString('utf8')
  if (req.url.endsWith('/responses')) {
    requests.push(JSON.parse(body))
    res.writeHead(200, { 'Content-Type': 'text/event-stream' })
    res.end('data: ' + JSON.stringify({ type: 'response.created', sequence_number: 1,
      response: { id: 'resp_test', object: 'response', model: 'muse-probe', status: 'in_progress', output: [] } }) + '\n\ndata: ' + JSON.stringify({ type: 'response.output_text.delta', sequence_number: 2,
      output_index: 0, item_id: 'msg_test', content_index: 0, delta: 'OK' }) + '\n\ndata: ' + JSON.stringify({
      type: 'response.completed', sequence_number: 3, response: { id: 'resp_test', object: 'response', model: 'muse-probe', status: 'completed', output: [],
        usage: { input_tokens: 1, output_tokens: 0, total_tokens: 1 } }
    }) + '\n\n')
  } else {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ object: 'list', data: [{ id: 'muse-probe', object: 'model',
      metadata: { 'muse-code': { release_date: '2026-01-01', is_hidden: false,
        limit: { context: 100000, output: 1024 } } } }] }))
  }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const endpoint = `http://127.0.0.1:${server.address().port}/v1`
await writeFile(path.join(state, 'muse/config/muse/settings.json'), JSON.stringify({
  schema_version: 1, model: 'muse-probe',
  endpoint_transport: { base_url: endpoint, auth: 'bearer' }
}))
// A fake credential, confined to the loopback provider in this test.
await writeFile(path.join(state, 'muse/config/muse/auth.json'), JSON.stringify({
  schema_version: 1, providers: { meta: { api_key: 'test-not-a-real-key' } }
}))
const child = spawn(command, [], {
  cwd: state,
  detached: true,
  env: { PATH: process.env.PATH, HOME: state, MUSE_HOME: path.join(state, 'muse'),
    MUSE_SERVE_ARGS: '--disable-sandbox', MUSE_CLI: '/not-the-pinned-cli' },
  stdio: ['pipe', 'pipe', 'pipe']
})
let stderr = ''
child.stderr.on('data', chunk => { stderr += chunk })
const pending = new Map()
let id = 0
let buffer = ''
child.stdout.on('data', chunk => {
  buffer += chunk
  let newline
  while ((newline = buffer.indexOf('\n')) >= 0) {
    const line = buffer.slice(0, newline); buffer = buffer.slice(newline + 1)
    if (!line.trim()) continue
    const message = JSON.parse(line)
    const waiter = pending.get(message.id)
    if (waiter) {
      pending.delete(message.id)
      if (message.error) waiter.reject(new Error(JSON.stringify(message.error)))
      else waiter.resolve(message.result)
    }
  }
})
function request(method, params) {
  return new Promise((resolve, reject) => {
    const current = ++id
    pending.set(current, { resolve, reject })
    child.stdin.write(JSON.stringify({ jsonrpc: '2.0', id: current, method, params }) + '\n')
  })
}
child.on('exit', code => {
  for (const waiter of pending.values()) waiter.reject(new Error(`adapter exited ${code}: ${stderr}`))
})
function stop() {
  try { process.kill(-child.pid, 'SIGKILL') } catch (error) {
    if (error.code !== 'ESRCH') throw error
  }
}
const exited = new Promise(resolve => child.once('exit', resolve))
const timeout = setTimeout(stop, 45_000)
const mcp = `
const { createInterface } = require('node:readline');
createInterface({input:process.stdin}).on('line', line => {
 const req=JSON.parse(line); if(req.id===undefined) return;
 let result={};
 if(req.method==='initialize') result={protocolVersion:req.params.protocolVersion,capabilities:{tools:{}},serverInfo:{name:'probe',version:'1'}};
 if(req.method==='tools/list') require('node:fs').writeFileSync(process.argv[1], 'connected');
 if(req.method==='tools/list') result={tools:[{name:'client_probe',description:'Client-provided tool',inputSchema:{type:'object',properties:{}}}]};
 console.log(JSON.stringify({jsonrpc:'2.0',id:req.id,result}));
});`
try {
  await writeFile(path.join(state, 'AGENTS.md'), 'LATTICE_MUSE_WORKSPACE_CANARY')
  await request('initialize', { protocolVersion: 1, clientCapabilities: {} })
  const session = await request('session/new', { cwd: state,
    mcpServers: mode === 'tools' ? [{ name: 'openai-functions', command: process.execPath, args: ['-e', mcp, path.join(state, 'mcp-connected')], env: [] }] : [] })
  await request('session/prompt', { sessionId: session.sessionId, prompt: [{type:'text',text:'Hello'}] })
  if (mode === 'tools') assert.equal(await readFile(path.join(state, 'mcp-connected'), 'utf8'), 'connected')
  assert.ok(requests.length, 'adapter must send a model request')
  const names = requests.flatMap(req => req.tools ?? []).flatMap(tool =>
    tool.type === 'namespace' ? tool.tools.map(item => `${tool.name}.${item.name}`) : [tool.name ?? tool.type])
  if (mode === 'tools') assert.ok(names.some(name => name.includes('client_probe')), `MCP tool missing: ${names}`)
  else assert.deepEqual(names, [], 'a tool-free request must expose no tools')
  assert.ok(names.every(name => name === 'mcp__openai_functions.client_probe' || name === 'muse.mcp__openai_functions__client_probe'),
    `unexpected built-in tools exposed: ${names}`)
  // Local filesystem/tool descriptions and workspace instructions must not
  // leak into the model's context, even if the caller supplies a workspace.
  const bodies = JSON.stringify(requests)
  assert.ok(!bodies.includes('LATTICE_MUSE_WORKSPACE_CANARY'), 'workspace rules leaked to the model')
  console.log('Model-visible tools:', [...new Set(names)].join(', '))
} finally {
  clearTimeout(timeout)
  child.stdin.end()
  const killTimeout = setTimeout(stop, 12_000)
  await exited
  clearTimeout(killTimeout)
  server.closeAllConnections()
  await new Promise(resolve => server.close(resolve))
  await rm(state, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 })
}
