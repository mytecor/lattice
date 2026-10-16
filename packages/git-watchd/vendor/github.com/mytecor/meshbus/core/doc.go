// Package core defines transport-independent authenticated peer messaging,
// bounded peer discovery, and best-effort one-hop pub/sub composition.
//
// Sender authority always comes from the authenticated transport session.
// Discovery metadata and identities serialized inside payloads are advisory
// data and never replace that authority.
package core
