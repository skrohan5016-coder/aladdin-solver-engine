package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

func TestRecorderWritesVersionedPrivateEvidence(t *testing.T) {
	dir := t.TempDir()
	recorder, err := New(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 16, 17, 45, 0, 123, time.UTC)
	recorder.now = func() time.Time { return fixed }

	auctionID := "auction-42"
	auction := &api.Auction{ID: &auctionID, EffectiveGasPrice: "1"}
	result := solve.Result{
		Solutions: []api.Solution{},
		Stats:     solve.Stats{Orders: 3, Solutions: 0},
	}
	if err := recorder.Auction(auctionID, auction, result, 12*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	notification := api.Notification{
		AuctionID:  auctionID,
		SolutionID: json.Number("7"),
		Kind:       "success",
		Extra: map[string]json.RawMessage{
			"rank": json.RawMessage(`2`),
		},
	}
	if err := recorder.Notification(notification); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	auctionPath := filepath.Join(dir, "auctions-2026-08-16.jsonl")
	auctionData, err := os.ReadFile(auctionPath)
	if err != nil {
		t.Fatal(err)
	}
	var auctionRecord AuctionRecord
	if err := json.Unmarshal(auctionData, &auctionRecord); err != nil {
		t.Fatal(err)
	}
	if auctionRecord.Schema != AuctionRecordSchema || auctionRecord.Auction == nil {
		t.Fatalf("unexpected auction record: %+v", auctionRecord)
	}

	notificationPath := filepath.Join(dir, "notifications-2026-08-16.jsonl")
	notificationData, err := os.ReadFile(notificationPath)
	if err != nil {
		t.Fatal(err)
	}
	var notificationRecord NotificationRecord
	if err := json.Unmarshal(notificationData, &notificationRecord); err != nil {
		t.Fatal(err)
	}
	if notificationRecord.Schema != NotificationRecordSchema {
		t.Fatalf("unexpected notification schema %q", notificationRecord.Schema)
	}
	if notificationRecord.Notify.SolutionID.String() != "7" {
		t.Fatalf("notification solution id changed: %+v", notificationRecord.Notify)
	}
	if _, ok := notificationRecord.Notify.Extra["rank"]; !ok {
		t.Fatalf("notification metadata was lost: %+v", notificationRecord.Notify)
	}

	if runtime.GOOS != "windows" {
		assertMode(t, dir, 0o750)
		assertMode(t, auctionPath, 0o640)
		assertMode(t, notificationPath, 0o640)
	}
}

func TestRecorderRotatesUsingTheRecordTimestamp(t *testing.T) {
	dir := t.TempDir()
	recorder, err := New(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	current := time.Date(2026, 8, 16, 23, 59, 59, 0, time.UTC)
	recorder.now = func() time.Time { return current }
	if err := recorder.Notification(api.Notification{AuctionID: "a", Kind: "success"}); err != nil {
		t.Fatal(err)
	}
	current = current.Add(2 * time.Second)
	if err := recorder.Notification(api.Notification{AuctionID: "b", Kind: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	for _, day := range []string{"2026-08-16", "2026-08-17"} {
		if _, err := os.Stat(filepath.Join(dir, "notifications-"+day+".jsonl")); err != nil {
			t.Fatalf("missing rotated file for %s: %v", day, err)
		}
	}
}

func TestRecorderReturnsOpenFailure(t *testing.T) {
	dir := t.TempDir()
	recorder, err := New(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	recorder.now = func() time.Time { return fixed }

	// A directory at the expected file path makes OpenFile fail on every OS
	// without depending on process privileges.
	blocked := filepath.Join(dir, "auctions-2026-08-16.jsonl")
	if err := os.Mkdir(blocked, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Auction("a", &api.Auction{}, solve.Result{}, time.Second); err == nil {
		t.Fatal("expected evidence write failure to be returned")
	}
}

func TestRecorderCloseIsIdempotent(t *testing.T) {
	recorder, err := New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode %04o, want %04o", path, got, want)
	}
}
