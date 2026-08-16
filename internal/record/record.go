// Package record persists every auction seen and every notification received.
//
// This is the point of running shadow at all: without a durable local record
// you cannot answer "how often would we have beaten the winner", which is the
// only number that decides whether going live is worth anything.
package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

type Recorder struct {
	mu       sync.Mutex
	dir      string
	day      string
	auctions *os.File
	notifs   *os.File
	// KeepAuctions stores the full auction payload. Useful for offline
	// replay, expensive on disk: roughly a few MB per auction on mainnet.
	KeepAuctions bool
}

func New(dir string, keepAuctions bool) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Recorder{dir: dir, KeepAuctions: keepAuctions}, nil
}

type AuctionRecord struct {
	Timestamp string         `json:"ts"`
	AuctionID string         `json:"auctionId"`
	ElapsedMs int64          `json:"elapsedMs"`
	Stats     solve.Stats    `json:"stats"`
	Solutions []api.Solution `json:"solutions"`
	Auction   *api.Auction   `json:"auction,omitempty"`
}

func (r *Recorder) Auction(id string, a *api.Auction, res solve.Result, elapsed time.Duration) {
	rec := AuctionRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		AuctionID: id,
		ElapsedMs: elapsed.Milliseconds(),
		Stats:     res.Stats,
		Solutions: res.Solutions,
	}
	if r.KeepAuctions {
		rec.Auction = a
	}
	r.write(&r.auctions, "auctions", rec)
}

type NotificationRecord struct {
	Timestamp string           `json:"ts"`
	Notify    api.Notification `json:"notify"`
}

func (r *Recorder) Notification(n api.Notification) {
	r.write(&r.notifs, "notifications", NotificationRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Notify:    n,
	})
}

// write appends one JSON line, rotating the file at UTC midnight.
func (r *Recorder) write(f **os.File, name string, v any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	day := time.Now().UTC().Format("2006-01-02")
	if day != r.day {
		r.closeLocked()
		r.day = day
	}
	if *f == nil {
		path := filepath.Join(r.dir, name+"-"+r.day+".jsonl")
		h, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		*f = h
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = (*f).Write(append(b, '\n'))
}

func (r *Recorder) closeLocked() {
	for _, f := range []**os.File{&r.auctions, &r.notifs} {
		if *f != nil {
			_ = (*f).Close()
			*f = nil
		}
	}
}

func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
}
