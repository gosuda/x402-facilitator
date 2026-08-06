package facilitator

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func entry(hash string, at time.Time) PendingSettlement {
	return PendingSettlement{
		Hash: hash, Payer: "0xpayer", PayTo: "0xpayee",
		Asset: "0xtoken", Amount: "20000", BroadcastAt: at,
	}
}

// What a restart inherits is exactly what was broadcast and never resolved.
func TestPendingLogSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending.json")
	now := time.Now().UTC()

	first, err := NewPendingLog(path)
	if err != nil {
		t.Fatal(err)
	}
	first.Add(entry("0xaaa", now.Add(-2*time.Minute)))
	first.Add(entry("0xbbb", now.Add(-time.Minute)))
	first.Resolve("0xaaa") // this one's outcome was seen; the other was not

	restarted, err := NewPendingLog(path)
	if err != nil {
		t.Fatal(err)
	}
	got := restarted.Unresolved()
	if len(got) != 1 || got[0].Hash != "0xbbb" {
		t.Fatalf("inherited %v, want only the unresolved 0xbbb", got)
	}
	if got[0].Amount != "20000" || got[0].PayTo != "0xpayee" {
		t.Fatalf("the record lost what it was for: %+v", got[0])
	}
}

// Oldest first, because the one that has been waiting longest is the one an operator wants
// resolved first.
func TestPendingLogOrdersByAge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending.json")
	p, _ := NewPendingLog(path)
	now := time.Now().UTC()
	p.Add(entry("0xnew", now))
	p.Add(entry("0xold", now.Add(-time.Hour)))
	p.Add(entry("0xmid", now.Add(-time.Minute)))

	got := p.Unresolved()
	want := []string{"0xold", "0xmid", "0xnew"}
	for i, w := range want {
		if got[i].Hash != w {
			t.Fatalf("position %d: got %s, want %s", i, got[i].Hash, w)
		}
	}
}

// A crash mid-write must not stop the next process from booting. A facilitator that refuses to
// start has a worse problem than one missing a record.
func TestPendingLogToleratesACorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending.json")
	if err := os.WriteFile(path, []byte(`[{"hash":"0xaaa","broadca`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewPendingLog(path)
	if err != nil {
		t.Fatalf("a truncated file stopped the boot: %v", err)
	}
	if len(p.Unresolved()) != 0 {
		t.Fatal("half a record was read as a settlement")
	}
	p.Add(entry("0xccc", time.Now().UTC()))
	if len(p.Unresolved()) != 1 {
		t.Fatal("the log did not recover for writing")
	}
}

// No path configured means no record - and no crash for asking.
func TestPendingLogDisabledIsInert(t *testing.T) {
	p, err := NewPendingLog("")
	if err != nil {
		t.Fatal(err)
	}
	p.Add(entry("0xaaa", time.Now()))
	p.Resolve("0xaaa")
	if len(p.Unresolved()) != 0 {
		t.Fatal("a disabled log kept something")
	}
}

// The write must be atomic: a reader never sees half a file.
func TestPendingLogWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending.json")
	p, _ := NewPendingLog(path)
	p.Add(entry("0xaaa", time.Now().UTC()))

	names, _ := os.ReadDir(dir)
	for _, n := range names {
		if filepath.Ext(n.Name()) == ".tmp" {
			t.Fatalf("a temporary file was left behind: %s", n.Name())
		}
	}
	if _, err := NewPendingLog(path); err != nil {
		t.Fatalf("the written file was not readable: %v", err)
	}
}
