package facilitator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ClaimStore is an in-memory, concurrency-safe single-use claim set. A block
// hash that has already been settled is refused on every later settle, so one
// payment binds to one resource exactly once. It forgets on restart: use FileClaimStore (or any Claimer) when a
// restart must not make a spent block spendable again.
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

// Claimer is the single-use claim contract the facilitator needs. ClaimStore
// is the in-memory implementation; FileClaimStore survives restarts. A
// Claim that cannot be made durable MUST return false (refuse), never true:
// a spent block that is forgotten is a payment that can be sold twice.
type Claimer interface {
	Claim(blockHash string) bool
	Spent(blockHash string) bool
}

var (
	_ Claimer = (*ClaimStore)(nil)
	_ Claimer = (*FileClaimStore)(nil)
)

// FileClaimStore is a ClaimStore whose history survives a restart. Every
// claim is appended to a JSON-lines file and fsynced BEFORE Claim reports
// success, so a crash after the append refuses a retry (safe) and a crash
// before it never reported success (also safe). On open the file is replayed;
// a torn final line (a crash mid-write) is ignored, any other unreadable line
// is an error rather than a silently shorter history.
type FileClaimStore struct {
	mu    sync.Mutex
	spent map[string]struct{}
	f     *os.File
}

type claimRecord struct {
	Hash string `json:"hash"`
	At   string `json:"at"`
}

// NewFileClaimStore opens (creating if needed, mode 0600) the claim log at path
// and loads its history.
func NewFileClaimStore(path string) (*FileClaimStore, error) {
	if path == "" {
		return nil, errors.New("claim store path is empty")
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	s := &FileClaimStore{spent: make(map[string]struct{}), f: f}
	data, err := os.ReadFile(path)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec claimRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil || rec.Hash == "" {
			if i == len(lines)-1 { // no trailing newline: a torn last write
				continue
			}
			_ = f.Close()
			return nil, fmt.Errorf("claim log %s line %d is unreadable: refusing to start with a shorter history", path, i+1)
		}
		s.spent[rec.Hash] = struct{}{}
	}
	// Drop a torn tail so the next append starts on a clean line; it was
	// never reported as claimed, so nothing the caller relied on is lost.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		keep := strings.LastIndexByte(string(data), '\n') + 1
		if err := f.Truncate(int64(keep)); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	return s, nil
}

// Claim atomically records blockHash as spent, durably. It returns false for a
// replay and also false when the claim could not be written.
func (s *FileClaimStore) Claim(blockHash string) bool {
	if s == nil || s.f == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.spent[blockHash]; ok {
		return false
	}
	line, err := json.Marshal(claimRecord{Hash: blockHash, At: time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return false
	}
	if _, err := s.f.Write(append(line, '\n')); err != nil {
		return false
	}
	if err := s.f.Sync(); err != nil {
		return false
	}
	s.spent[blockHash] = struct{}{}
	return true
}

// Spent reports whether blockHash has been claimed.
func (s *FileClaimStore) Spent(blockHash string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.spent[blockHash]
	return ok
}

// Close releases the log file.
func (s *FileClaimStore) Close() error {
	if s == nil || s.f == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.f.Close()
	s.f = nil
	return err
}
