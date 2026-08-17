package record

import (
	"strings"
	"testing"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/buildinfo"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

const testEngineCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestFullAuctionRecordingRequiresExactEmbeddedEngineCommit(t *testing.T) {
	original := buildinfo.Commit
	buildinfo.Commit = "unknown"
	t.Cleanup(func() { buildinfo.Commit = original })

	_, err := NewWithOptions(t.TempDir(), Options{
		KeepAuctions: true,
		Config:       solve.DefaultConfig(),
	})
	if err == nil || !strings.Contains(err.Error(), "requires an exact embedded engine commit") {
		t.Fatalf("invalid full-recording identity was accepted: %v", err)
	}
}

func TestEngineCommitOptionCannotOverrideEmbeddedIdentity(t *testing.T) {
	original := buildinfo.Commit
	buildinfo.Commit = testEngineCommit
	t.Cleanup(func() { buildinfo.Commit = original })

	other := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	_, err := NewWithOptions(t.TempDir(), Options{
		KeepAuctions: false,
		Config:       solve.DefaultConfig(),
		EngineCommit: other,
	})
	if err == nil || !strings.Contains(err.Error(), "assertion mismatch") {
		t.Fatalf("engine source override was accepted: %v", err)
	}
}

func TestFullAuctionRecordingUsesEmbeddedIdentity(t *testing.T) {
	original := buildinfo.Commit
	buildinfo.Commit = testEngineCommit
	t.Cleanup(func() { buildinfo.Commit = original })

	recorder, err := NewWithOptions(t.TempDir(), Options{
		KeepAuctions: true,
		Config:       solve.DefaultConfig(),
		EngineCommit: testEngineCommit,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	if recorder.identity.Engine.Commit != testEngineCommit {
		t.Fatalf("recorded source = %q, want %q", recorder.identity.Engine.Commit, testEngineCommit)
	}
}
