package facilitator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Settlements a restart would otherwise forget.
//
// Between broadcasting and confirming, a facilitator holds a fact nobody else has: that it sent
// this transaction, for this payment. Lose that to a restart and the transaction still settles -
// the chain does not care that we stopped watching - but nobody is left who can connect the money
// that moved to the request it answered. The seller sees a timeout, the payer sees a debit, and
// reconciling them becomes archaeology.
//
// So the hash is written down before broadcast and cleared once the outcome is known. What remains
// on disk after a crash is precisely the set of unfinished settlements, which the next process
// resolves against the chain rather than guessing at.
//
// A file rather than a database on purpose: this is small, it is per-instance by nature (the
// instance is the only one holding the nonce), and an operator can read it.

// PendingSettlement is one broadcast whose outcome was not yet recorded.
type PendingSettlement struct {
	Hash        string    `json:"hash"`
	Payer       string    `json:"payer"`
	PayTo       string    `json:"payTo"`
	Asset       string    `json:"asset"`
	Amount      string    `json:"amount"`
	BroadcastAt time.Time `json:"broadcastAt"`
}

// PendingLog is a crash-safe record of settlements in flight. The zero value is usable and does
// nothing, so a facilitator configured without a path simply keeps no log.
type PendingLog struct {
	path string
	mu   sync.Mutex
	open map[string]PendingSettlement
}

// NewPendingLog opens (or creates) the log at path. An empty path disables recording, which is
// the right default for a throwaway instance and the wrong one for anything holding gas.
func NewPendingLog(path string) (*PendingLog, error) {
	p := &PendingLog{path: path, open: map[string]PendingSettlement{}}
	if path == "" {
		return p, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return nil, err
	}
	var entries []PendingSettlement
	// A truncated file is a crash mid-write, not a reason to refuse to start: an operator whose
	// facilitator will not boot has a worse problem than one missing a reconciliation record.
	if err := json.Unmarshal(buf, &entries); err == nil {
		for _, e := range entries {
			p.open[e.Hash] = e
		}
	}
	return p, nil
}

// Add records a broadcast before anyone waits on it.
func (p *PendingLog) Add(entry PendingSettlement) {
	if p == nil || p.path == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.open[entry.Hash] = entry
	p.flush()
}

// Resolve forgets a settlement whose outcome is now known and reported.
func (p *PendingLog) Resolve(hash string) {
	if p == nil || p.path == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.open, hash)
	p.flush()
}

// Unresolved returns what a restart inherited, oldest first.
func (p *PendingLog) Unresolved() []PendingSettlement {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]PendingSettlement, 0, len(p.open))
	for _, e := range p.open {
		out = append(out, e)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].BroadcastAt.Before(out[j-1].BroadcastAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// flush writes through a temporary file and renames, so a crash mid-write leaves either the old
// contents or the new ones - never half of each. Callers hold the lock.
func (p *PendingLog) flush() {
	entries := make([]PendingSettlement, 0, len(p.open))
	for _, e := range p.open {
		entries = append(entries, e)
	}
	buf, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, p.path)
}
