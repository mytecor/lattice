// Regression tests for the acp-normalizer id-stability core (normalize.mjs).
//
// Reads ./normalize.mjs directly, so it runs without a build and without a
// build tool. Wired into `nix flake check` via the acp-normalizer test
// derivation (see package.nix / tests wiring).
//
// The two behaviours being pinned here are the contract:
//   1. Consecutive agent_message_chunk / agent_thought_chunk of ONE logical
//      assistant reply are glued under a single stable messageId (the reason
//      the transformer exists — clients like superlite key rendering on
//      messageId and would otherwise print each streamed chunk as its own
//      message).
//   2. ANY non-chunk sessionUpdate — in particular the kinds the live transform
//      stream delivers between turns (usage_update, session_info_update,
//      available_commands_update) — closes the current logical message, so the
//      NEXT turn starts a FRESH messageId.
//
// Case 2 is the regression. The v0.1.0 code reset its id only on a fixed
// allow-list (prompt_received / tool_call / user_message_chunk / agent_message
// / agent_thought / turn_complete). The live stream does not carry those between
// turns, so the last id of turn N leaked unchanged into turn N+1 and a
// multi-turn session recorded every turn under the SAME messageId. On
// `session/load` that replay collapsed distinct turns into one broken message.
import assert from 'node:assert/strict'
import { createNormalize } from './normalize.mjs'

// --- helpers -------------------------------------------------------------

// Feed a sequence of raw sessionUpdate kinds (with per-chunk fresh messageIds,
// the shape hydra `wp()` produces) through a fresh normalizer state, and return
// the messageId each agent chunk was re-stamped with.
function run(kinds) {
  const { normalize } = createNormalize()
  const stamped = []
  let n = 0
  for (const [kind, msgId] of kinds) {
    const envelope = { sessionId: 's1', update: { sessionUpdate: kind, messageId: msgId } }
    const out = normalize(envelope)
    if (kind === 'agent_message_chunk' || kind === 'agent_thought_chunk') {
      stamped.push(out ? out.update.messageId : msgId)
    }
  }
  return stamped
}

// --- test 1: one streamed reply stays one message -------------------------

{
  // A single reply streamed as two chunks, each carrying a fresh per-token id.
  // Both must be re-stamped to the FIRST chunk's id (stable).
  const ids = run([
    ['agent_message_chunk', 'tok-a'],
    ['agent_message_chunk', 'tok-b'],
    ['agent_message_chunk', 'tok-c'],
  ])
  // tok-a is adopted as the stable id and returned unchanged; tok-b/tok-c are
  // rewritten back to tok-a.
  assert.equal(ids[0], 'tok-a')
  assert.equal(ids[1], 'tok-a')
  assert.equal(ids[2], 'tok-a')
  assert.deepEqual(new Set(ids), new Set(['tok-a']), 'chunks of one reply must share one id')
}

// --- test 2 (regression): a fresh id starts after a non-chunk boundary ----

{
  // Simulation of the observed live failure: turn 1 ends with a usage+info
  // update (no prompt_received/turn_complete in the live stream), then turn 2
  // starts. Turn 2 MUST get a different id from turn 1.
  const ids = run([
    // turn 1
    ['agent_message_chunk', 'turn1-a'],
    ['agent_message_chunk', 'turn1-b'],
    // inter-turn traffic actually delivered by the live transform stream
    ['usage_update', null],
    ['session_info_update', null],
    // turn 2
    ['agent_message_chunk', 'turn2-a'],
    ['agent_message_chunk', 'turn2-b'],
  ])
  // ids in order: turn1-a, turn1-b, turn2-a, turn2-b
  assert.deepEqual(new Set(ids.slice(0, 2)), new Set(['turn1-a']), 'turn 1 must be one id')
  assert.deepEqual(new Set(ids.slice(2)), new Set(['turn2-a']), 'turn 2 must be one id')
  assert.notEqual(ids[2], ids[0], 'turn 2 must start a FRESH id, not reuse turn 1 id')
}

// --- test 3: tool_call boundary opens a new logical message inside a turn --

{
  const ids = run([
    ['agent_message_chunk', 'm-a'],
    ['tool_call', null],
    ['agent_message_chunk', 'm-b'],
  ])
  assert.equal(ids.length, 2)
  assert.notEqual(ids[1], ids[0], 'a reply before and after a tool_call must differ')
}

// --- test 4: thought chunks glue to the same logical reply ----------------

{
  // agent_thought_chunk then agent_message_chunk of the same reply share the id.
  const ids = run([
    ['agent_thought_chunk', 't-a'],
    ['agent_thought_chunk', 't-b'],
    ['agent_message_chunk', 't-c'],
  ])
  assert.deepEqual(new Set(ids), new Set(['t-a']), 'thought+message of one reply share one id')
}

// --- test 5: non-object / missing update is tolerated ---------------------

{
  const { normalize } = createNormalize()
  assert.equal(normalize(null), null)
  assert.equal(normalize({}), null)
  assert.equal(normalize({ update: 'not-an-object' }), null)
  assert.equal(normalize({ update: { sessionUpdate: 'anything' } }), null)
}

console.log('acp-normalizer normalize.test.mjs: all assertions passed')
