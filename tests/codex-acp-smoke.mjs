import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, mkdir, writeFile, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import http from 'node:http'

const [command] = process.argv.slice(2)
assert.ok(command, 'provide codex-acp-fsless executable')
const state = await mkdtemp(path.join(tmpdir(), 'codex-fsless-'))
await mkdir(path.join(state, 'codex'))
const requests = []
const server = http.createServer(async (req, res) => {
  let body = ''
  for await (const chunk of req) body += chunk
  if (req.url.endsWith('/responses')) {
    requests.push(JSON.parse(body))
    res.writeHead(200, { 'Content-Type': 'text/event-stream' })
    res.end('event: response.completed\ndata: ' + JSON.stringify({
      type: 'response.completed', response: { id: 'resp_test', status: 'completed', output: [],
        usage: { input_tokens: 1, output_tokens: 0, total_tokens: 1 } }
    }) + '\n\n')
  } else {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ data: [] }))
  }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const endpoint = `http://127.0.0.1:${server.address().port}/v1`
await writeFile(path.join(state, 'codex/config.toml'), `
model_provider = "probe"
model = "gpt-5.4"
[features]
enable_request_compression = false
[model_providers.probe]
name = "probe"
base_url = "${endpoint}"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
`)
const child = spawn(command, [], {
  cwd: state,
  detached: true,
  env: { PATH: process.env.PATH, HOME: state, CODEX_HOME: path.join(state, 'codex'),
    RUST_LOG: 'error' },
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
  await request('initialize', { protocolVersion: 1, clientCapabilities: {} })
  const environments = await readFile(path.join(state, 'codex/environments.toml'), 'utf8')
  assert.match(environments, /default\s*=\s*"none"/)
  assert.match(environments, /include_local\s*=\s*true/)
  const session = await request('session/new', { cwd: state,
    mcpServers: [{ name: 'probe', command: process.execPath, args: ['-e', mcp, path.join(state, 'mcp-connected')], env: [] }] })
  await request('session/prompt', { sessionId: session.sessionId, prompt: [{type:'text',text:'Hello'}] })
  assert.equal(await readFile(path.join(state, 'mcp-connected'), 'utf8'), 'connected')
  assert.ok(requests.length, 'adapter must send a model request')
  const names = requests.flatMap(req => req.tools ?? []).flatMap(tool =>
    tool.type === 'namespace' ? tool.tools.map(item => `${tool.name}.${item.name}`) : [tool.name ?? tool.type])
  assert.ok(names.some(name => name.includes('client_probe')), `MCP tool missing: ${names}`)
  // Allow only the explicitly documented non-environment helpers. New native
  // capabilities must fail this contract check when upstream is upgraded.
  const helpers = new Set(['list_mcp_resources', 'list_mcp_resource_templates',
    'read_mcp_resource', 'update_plan', 'request_user_input'])
  assert.ok(names.every(name => helpers.has(name) || name === 'mcp__probe.client_probe'),
    `unexpected tools exposed: ${names}`)
  console.log('Model-visible tools:', [...new Set(names)].join(', '))
} finally {
  clearTimeout(timeout)
  stop()
  await exited
  server.closeAllConnections()
  await new Promise(resolve => server.close(resolve))
  await rm(state, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 })
}
