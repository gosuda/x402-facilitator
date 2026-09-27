package facilitator

import "sync"

// ClaimStore is an in-memory, concurrency-safe single-use claim set. A block
// hash that has already been settled is refused on every later settle, so one
// payment binds to one resource exactly once. Persistence across restarts is
// out of scope for this minimal facilitator (the block itself remains the
// authoritative receipt on the public ledger); deployments that need durable
// anti-replay can swap this for a KV/DB-backed store with the same interface.
type ClaimStore struct {
	mu    sync.Mutex
	spent map[string]struct{}
}

// NewClaimStore returns an empty ClaimStore.
func NewClaimStore() *ClaimStore {
	return &ClaimStore{spent: make(map[string]struct{})}
}

// Claim atomically marks blockHash as spent. It returns true when this is the
// first claim for the hash and false when it was already spent (replay).
func (s *ClaimStore) Claim(blockHash string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.spent[blockHash]; ok {
		return false
	}
	s.spent[blockHash] = struct{}{}
	return true
}

// Spent reports whether blockHash has already been claimed.
func (s *ClaimStore) Spent(blockHash string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.spent[blockHash]
	return ok
}
