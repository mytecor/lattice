package core

func (n *Node) Discovered(peer Peer) error {
	if authenticated, ok := n.peers.Get(peer.ID); ok {
		authenticated.Metadata = peer.Metadata
		authenticated.Hops = peer.Hops
		return n.recordPeer(n.peers, authenticated)
	}
	return n.recordPeer(n.candidates, peer)
}

func (n *Node) Authenticated(id PeerID) error {
	_, known := n.peers.Get(id)
	peer, ok := n.candidates.Get(id)
	if !ok {
		peer = Peer{ID: id}
	}
	if err := n.recordPeer(n.peers, peer); err != nil {
		return err
	}
	n.candidates.Remove(id)
	if !known {
		go func() {
			if n.isRunning() {
				n.syncInterests(id)
			}
		}()
	}
	return nil
}

func (n *Node) recordPeer(directory *PeerDirectory, peer Peer) error {
	err := directory.Remember(peer)
	n.reportPeerError(err)
	return err
}

func (n *Node) peerIDs() []PeerID { return n.peers.IDs() }

func (n *Node) reportPeerError(err error) {
	if err != nil && n.onPeerError != nil {
		n.onPeerError(err)
	}
}
