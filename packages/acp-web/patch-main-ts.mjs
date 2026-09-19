// Lattice patch (f13-01 step 4): pre-configure a default ACP agent in the
// acp-components demo. Upstream ships an empty `builtinAgents` list in
// production builds (it only fills the list during `vite dev` against a local
// dev agent). This script edits examples/demo/src/main.tsx so the LAN ACP
// ingress is always available without manual input: the default agent URL is
// derived from the serving hostname (acp-ui.<node>.local -> acp.<node>.local,
// the sibling mDNS host of this UI), with a build-time VITE_ACP_ENDPOINT
// override. A pure source edit — reproducible, and touches neither the daemon
// nor the Caddy ingress of the ACP endpoint.
//
// The patch keys on the exact upstream block so it fails loudly if upstream
// changes the shape we rely on (instead of silently building a UI without the
// default agent).
import { readFileSync, writeFileSync } from 'node:fs'

const mainTs = process.argv[2]

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

const newBlock =
  "const builtinAgents: AgentConfig[] = (() => {\n" +
  "  // Lattice (f13-01 step 4): pre-configured default ACP agent. The client is\n" +
  "  // served from acp-ui.<node>.local and talks to the sibling ACP ingress\n" +
  "  // acp.<node>.local. Derive the default endpoint from the serving hostname so\n" +
  "  // the bundle is node-agnostic; a build-time VITE_ACP_ENDPOINT overrides it.\n" +
  "  const override = import.meta.env.VITE_ACP_ENDPOINT as string | undefined;\n" +
  "  if (override) {\n" +
  "    return [{ id: 'acp', name: 'ACP Endpoint', transport: { type: 'websocket', url: override } }];\n" +
  "  }\n" +
  "  const host = typeof window !== 'undefined' ? window.location.hostname : '';\n" +
  "  if (host.startsWith('acp-ui.') && host.endsWith('.local')) {\n" +
  "    return [{ id: 'acp', name: 'ACP Endpoint', transport: { type: 'websocket', url: `ws://${host.replace(/^acp-ui\\./, 'acp.')}/` } }];\n" +
  "  }\n" +
  "  return [];\n" +
  '})();'

writeFileSync(mainTs, src.replace(oldBlock, newBlock))
console.log('acp-web: default ACP agent patched into main.tsx')
