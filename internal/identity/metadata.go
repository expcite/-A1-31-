package identity

// Metadata is a public read-only management observation. It is deliberately
// NOT TrustStore.Lookup and must never be used for handshake/admission: revoked
// and retired identities still have displayable public metadata, but Lookup
// continues refusing them. No private handles or scalars are returned.
func Metadata(store Store, id string) (Peer, error) {
	s, ok := store.(*fileStore)
	if !ok || !ValidName(id) {
		return Peer{}, ErrUnknownPeer
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.peers[id]
	if !ok {
		return Peer{}, ErrUnknownPeer
	}
	return p.clone(), nil
}
