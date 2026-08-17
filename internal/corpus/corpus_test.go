package corpus

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/buildinfo"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/contract"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/record"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

const testCommit = "1111111111111111111111111111111111111111"

func TestPackAndReplayAreDeterministic(t *testing.T) {
	recordPath := writeReplayRecord(t, true)
	firstDir := filepath.Join(t.TempDir(), "corpus")
	firstManifest, err := Pack(context.Background(), []string{recordPath}, firstDir, PackOptions{SourceCommit: testCommit})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstManifest.Entries) != 1 || firstManifest.RedactionPolicy != RedactSignatures {
		t.Fatalf("unexpected manifest: %+v", firstManifest)
	}
	auctionBytes, err := os.ReadFile(filepath.Join(firstDir, firstManifest.Entries[0].AuctionFile))
	if err != nil {
		t.Fatal(err)
	}
	var auction api.Auction
	if err := json.Unmarshal(auctionBytes, &auction); err != nil {
		t.Fatal(err)
	}
	if len(auction.Orders) != 1 || auction.Orders[0].Signature != "0x" {
		t.Fatalf("signature was not redacted: %+v", auction.Orders)
	}

	firstReport, err := Replay(context.Background(), firstDir, ReplayOptions{SourceCommit: testCommit})
	if err != nil {
		t.Fatal(err)
	}
	secondReport, err := Replay(context.Background(), firstDir, ReplayOptions{SourceCommit: testCommit})
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := ReportJSON(firstReport)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := ReportJSON(secondReport)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("replay report changed:\n%s\n%s", firstJSON, secondJSON)
	}

	secondDir := filepath.Join(t.TempDir(), "corpus")
	if _, err := Pack(context.Background(), []string{recordPath}, secondDir, PackOptions{SourceCommit: testCommit}); err != nil {
		t.Fatal(err)
	}
	firstManifestBytes, err := os.ReadFile(filepath.Join(firstDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	secondManifestBytes, err := os.ReadFile(filepath.Join(secondDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstManifestBytes, secondManifestBytes) {
		t.Fatalf("packing the same record changed manifest bytes:\n%s\n%s", firstManifestBytes, secondManifestBytes)
	}
}

func TestPackRejectsPartialFinalRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	if err := os.WriteFile(path, []byte(`{"schema":"`+record.AuctionRecordSchema+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Pack(context.Background(), []string{path}, filepath.Join(t.TempDir(), "corpus"), PackOptions{SourceCommit: testCommit})
	if err == nil || !strings.Contains(err.Error(), "partial final record") {
		t.Fatalf("partial record was not rejected: %v", err)
	}
}

func TestReplayRejectsCorruptionAndInventoryDrift(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, string, Manifest){
		"corrupt expected": func(t *testing.T, dir string, manifest Manifest) {
			path := filepath.Join(dir, manifest.Entries[0].ExpectedFile)
			if err := os.WriteFile(path, []byte(`{"schema":"corrupt"}`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"unlisted file": func(t *testing.T, dir string, _ Manifest) {
			if err := os.WriteFile(filepath.Join(dir, "extra.json"), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			recordPath := writeReplayRecord(t, true)
			dir := filepath.Join(t.TempDir(), "corpus")
			manifest, err := Pack(context.Background(), []string{recordPath}, dir, PackOptions{SourceCommit: testCommit})
			if err != nil {
				t.Fatal(err)
			}
			mutate(t, dir, manifest)
			if _, err := Replay(context.Background(), dir, ReplayOptions{SourceCommit: testCommit}); err == nil {
				t.Fatal("corrupt corpus was accepted")
			}
		})
	}
}

func TestReplayRejectsSourceMismatch(t *testing.T) {
	recordPath := writeReplayRecord(t, true)
	dir := filepath.Join(t.TempDir(), "corpus")
	if _, err := Pack(context.Background(), []string{recordPath}, dir, PackOptions{SourceCommit: testCommit}); err != nil {
		t.Fatal(err)
	}
	other := "2222222222222222222222222222222222222222"
	if _, err := Replay(context.Background(), dir, ReplayOptions{SourceCommit: other}); err == nil {
		t.Fatal("source mismatch was accepted")
	}
}

func TestReplayRejectsSymlinkedEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	recordPath := writeReplayRecord(t, true)
	dir := filepath.Join(t.TempDir(), "corpus")
	manifest, err := Pack(context.Background(), []string{recordPath}, dir, PackOptions{SourceCommit: testCommit})
	if err != nil {
		t.Fatal(err)
	}
	expectedPath := filepath.Join(dir, manifest.Entries[0].ExpectedFile)
	if err := os.Remove(expectedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(manifest.Entries[0].AuctionFile, expectedPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Replay(context.Background(), dir, ReplayOptions{SourceCommit: testCommit}); err == nil {
		t.Fatal("symlinked corpus entry was accepted")
	}
}

func TestPackRefusesExistingDestination(t *testing.T) {
	recordPath := writeReplayRecord(t, true)
	dir := filepath.Join(t.TempDir(), "corpus")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Pack(context.Background(), []string{recordPath}, dir, PackOptions{SourceCommit: testCommit}); err == nil {
		t.Fatal("existing destination was overwritten")
	}
}

func writeReplayRecord(t *testing.T, newline bool) string {
	t.Helper()
	auctionBytes, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "auction-direct.json"))
	if err != nil {
		t.Fatal(err)
	}
	var auction api.Auction
	if err := json.Unmarshal(auctionBytes, &auction); err != nil {
		t.Fatal(err)
	}
	config := solve.DefaultConfig()
	config.RequireProfitable = false
	configSnapshot, configDigest, err := solve.SnapshotConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	result := solve.Solve(context.Background(), &auction, config)
	item := record.AuctionRecord{
		Schema:    record.AuctionRecordSchema,
		Timestamp: "2026-08-17T00:00:00Z",
		AuctionID: "42001",
		ElapsedMs: 1,
		Stats:     result.Stats,
		Solutions: result.Solutions,
		Auction:   &auction,
		Identity: &record.ReplayIdentity{
			Engine: buildinfo.Identity{
				Commit:    testCommit,
				GoVersion: runtime.Version(),
				GOOS:      runtime.GOOS,
				GOARCH:    runtime.GOARCH,
			},
			UpstreamCommit: contract.UpstreamCommit,
			Config:         configSnapshot,
			ConfigSHA256:   configDigest,
		},
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if newline {
		encoded = append(encoded, '\n')
	}
	path := filepath.Join(t.TempDir(), "auctions.jsonl")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
