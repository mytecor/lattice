// Lattice patch (f13-01 step 4 + fix): pre-configure a default ACP agent in the
// acp-components demo. Upstream ships an empty `builtinAgents` list in
// production builds (it only fills the list during `vite dev` against a local
// dev agent). This script edits examples/demo/src/main.tsx so the ACP ingress
// is always available without manual input: the default agent URL is derived
// from the serving hostname (acp-ui.<host> -> acp.<host>, the sibling host of
// this UI — on the LAN mDNS acp-ui.<node>.local -> acp.<node>.local, on the
// mesh acp-ui.<meshDomain> -> acp.<meshDomain>), with a build-time
// VITE_ACP_ENDPOINT override. The websocket scheme follows the serving page:
// wss:// behind https (mesh), ws:// on plain http (LAN), so the derived
// endpoint never trips browser mixed-content blocking. A pure source edit —
// reproducible, and touches neither the daemon nor the Caddy ingress of the ACP
// endpoint.
//
// The patch keys on the exact upstream block so it fails loudly if upstream
// changes the shape we rely on (instead of silently building a UI without the
// default agent).
//
// Lattice fix (2026-09-20, step-5 acceptance finding): the agent config also
// carries `clientInfo`. Without it AcpClient.initialize serializes
// `clientInfo: clientInfo ?? null`, and the pinned hydra-acp 0.1.183 zod
// schema rejects `clientInfo: null` ("Expected object, received null"):
// initialize fails, the agent lands in status "error", and on every page
// reload the client never reaches session/list — so previously created
// sessions are not restored. Upstream hosts pass clientInfo; the demo's
// production builtinAgents leave it unset. Lattice sets it here, in the
// Lattice-owned patch, without touching upstream or the daemon (verified
// against the live endpoint: initialize with a clientInfo object is accepted).
import { readFileSync, writeFileSync } from 'node:fs'

const mainTs = process.argv[2]
const clientVersion = process.argv[3] ?? '0.1.0'

const src = readFileSync(mainTs, 'utf8')

const oldBlock =
  "const builtinAgents: AgentConfig[] = isLocalDev\n" +
  "  ? [{\n" +
  "      id: 'local-agent',\n" +
  "      name: 'Local Agent',\n" +
  "      transport: { type: 'websocket', url: LOCAL_AGENT_URL },\n" +
  "    }]\n" +
  '  : [];'

if (!src.includes(oldBlock)) {
  console.error('acp-web: builtinAgents block not found in main.tsx')
  process.exit(1)
}

const newBlock = [
  "const builtinAgents: AgentConfig[] = (() => {",
  "  // Lattice (f13-01 step 4): pre-configured default ACP agent. The client is",
  "  // served from acp-ui.<host> (LAN mDNS or mesh) and talks to the sibling",
  "  // ACP ingress acp.<host>. Derive the default endpoint from the serving",
  "  // hostname so the bundle is node-agnostic; a build-time VITE_ACP_ENDPOINT",
  "  // overrides it. The scheme is wss:// on https pages (mesh) and ws:// on",
  "  // plain http (LAN), so mixed-content is never tripped.",
  "  // clientInfo is required: without it AcpClient sends clientInfo: null and",
  "  // hydra-acp (zod) rejects initialize (\"Expected object, received null\").",
  "  const clientInfo = { name: 'acp-ui', version: '" + clientVersion + "' };",
  "  const override = import.meta.env.VITE_ACP_ENDPOINT as string | undefined;",
  "  if (override) {",
  "    return [{ id: 'acp', name: 'ACP Endpoint', clientInfo, transport: { type: 'websocket', url: override } }];",
  "  }",
  "  const host = typeof window !== 'undefined' ? window.location.hostname : '';",
  "  if (host.startsWith('acp-ui.')) {",
  "    const wss = typeof window !== 'undefined' && window.location.protocol === 'https:';",
  "    return [{ id: 'acp', name: 'ACP Endpoint', clientInfo, transport: { type: 'websocket', url: `${wss ? 'wss' : 'ws'}://${host.replace(/^acp-ui\\./, 'acp.')}/` } }];",
  "  }",
  "  return [];",
  '})();',
].join('\n')

writeFileSync(mainTs, src.replace(oldBlock, newBlock))
console.log('acp-web: default ACP agent patched into main.tsx')
