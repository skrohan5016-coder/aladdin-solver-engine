package record

import (
	"strings"
	"testing"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

func TestFullAuctionRecordingRequiresExactEngineCommit(t *testing.T) {
	_, err := NewWithOptions(t.TempDir(), Options{
		KeepAuctions: true,
		Config:       solve.DefaultConfig(),
		EngineCommit: "unknown",
	})
	if err == nil || !strings.Contains(err.Error(), "requires an exact engine commit") {
		t.Fatalf("invalid full-recording identity was accepted: %v", err)
	}
}
