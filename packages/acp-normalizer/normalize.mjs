// acp-normalizer — pure normalization core (unit-testable).
//
// Reassigns the per-token fresh `messageId` that hydra `wp()` stamps onto each
// `agent_message_chunk` / `agent_thought_chunk` to ONE stable id per logical
// assistant message, so clients that key rendering on `messageId` (e.g.
// superlite) render a streamed reply as a single message instead of one
// message per chunk.
//
// Only `agent_message_chunk` and `agent_thought_chunk` are treated as pieces
// of the SAME logical assistant message and glued together under one id. EVERY
// other `sessionUpdate` kind is a boundary that closes the current logical
// message and forces the next chunk to start a fresh id.
//
// This boundary rule is deliberately "everything that is not a chunk resets",
// NOT a fixed allow-list. hydra-acp's live transform stream does not deliver
// `prompt_received` / `user_message_chunk` / `turn_complete` between turns —
// the inter-turn traffic is `usage_update`, `session_info_update`,
// `available_commands_update` and friend. A fixed boundary allow-list (the
// v0.1.0 behaviour, keyed on friction kinds) never saw those kinds, so its
// current id leaked unchanged from one turn into the next, and every turn of a
// multi-turn session reset to the same id. That baked a cross-turn `messageId`
// collision into the recorded history: `session/load` replays it, and a client
// keying on `messageId` merges distinct turns into one broken message (the
// observed failure on the looped "Tool call." session). See the regression
// suite in `normalize.test.mjs`.

const CHUNK = new Set(['agent_message_chunk', 'agent_thought_chunk'])

/**
 * Build a normalizer bound to its own per-session id state.
 *
 * Returns `normalize(envelope)` — given a `response:session/update` envelope,
 * returns a rewritten envelope when the update's `messageId` should be
 * re-stamped to the current logical message's stable id, or `null` otherwise
 * (non-chunk updates, and chunks that already carry the current id).
 */
export function createNormalize() {
  // sessionId -> current logical message id
  const currentId = new Map()

  function normalize(envelope) {
    const update = envelope?.update
    if (!update || typeof update !== 'object' || Array.isArray(update)) return null
    const kind = update.sessionUpdate
    const sid = envelope.sessionId ?? '(none)'

    // Any non-chunk update closes the current logical assistant message. This
    // is the regression fix: inter-turn kinds such as `usage_update` or
    // `session_info_update` (which the live transform stream does deliver)
    // must reset the id exactly like the classic boundary kinds, otherwise the
    // last id of turn N is reused as the first id of turn N+1.
    if (!CHUNK.has(kind)) {
      currentId.delete(sid)
      return null
    }

    const before = update.messageId ?? null
    let id = currentId.get(sid)
    if (id === undefined) {
      id = before ?? crypto.randomUUID()
      currentId.set(sid, id)
    }
    if (before === id) return null
    return { ...envelope, update: { ...update, messageId: id } }
  }

  return { normalize }
}
