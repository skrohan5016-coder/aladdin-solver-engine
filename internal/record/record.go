// Package record persists shadow-auction evidence and driver notifications.
//
// Recording errors are returned to the caller and must be logged. Silently
// losing evidence would make coverage and validation reports untrustworthy.
package record

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

const (
	AuctionRecordSchema      = "aladdin-shadow-auction-record/v1"
	NotificationRecordSchema = "aladdin-shadow-notification-record/v1"
)

type Recorder struct {
	mu       sync.Mutex
	dir      string
	day      string
	auctions *os.File
	notifs   *os.File
	now      func() time.Time

	// KeepAuctions stores the complete auction payload for offline replay. It
	// can include order signatures and consumes substantial disk space, so it
	// is opt-in and the directory is private to the solver service account.
	KeepAuctions bool
}

func New(dir string, keepAuctions bool) (*Recorder, error) {
	if dir == "" {
		return nil, errors.New("record directory is empty")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create record directory: %w", err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		return nil, fmt.Errorf("secure record directory: %w", err)
	}
	return &Recorder{
		dir:          dir,
		KeepAuctions: keepAuctions,
		now:          time.Now,
	}, nil
}

type AuctionRecord struct {
	Schema    string         `json:"schema"`
	Timestamp string         `json:"ts"`
	AuctionID string         `json:"auctionId"`
	ElapsedMs int64          `json:"elapsedMs"`
	Stats     solve.Stats    `json:"stats"`
	Solutions []api.Solution `json:"solutions"`
	Auction   *api.Auction   `json:"auction,omitempty"`
}

func (r *Recorder) Auction(id string, auction *api.Auction, result solve.Result, elapsed time.Duration) error {
	now := r.currentTime()
	record := AuctionRecord{
		Schema:    AuctionRecordSchema,
		Timestamp: now.Format(time.RFC3339Nano),
		AuctionID: id,
		ElapsedMs: elapsed.Milliseconds(),
		Stats:     result.Stats,
		Solutions: result.Solutions,
	}
	if r.KeepAuctions {
		record.Auction = auction
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode auction record: %w", err)
	}
	return r.write(&r.auctions, "auctions", now, data)
}

type NotificationRecord struct {
	Schema    string           `json:"schema"`
	Timestamp string           `json:"ts"`
	Notify    api.Notification `json:"notify"`
}

func (r *Recorder) Notification(notification api.Notification) error {
	now := r.currentTime()
	record := NotificationRecord{
		Schema:    NotificationRecordSchema,
		Timestamp: now.Format(time.RFC3339Nano),
		Notify:    notification,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode notification record: %w", err)
	}
	return r.write(&r.notifs, "notifications", now, data)
}

func (r *Recorder) currentTime() time.Time {
	if r.now == nil {
		return time.Now().UTC()
	}
	return r.now().UTC()
}

// write appends one complete JSON line and rotates files at UTC midnight.
func (r *Recorder) write(file **os.File, name string, now time.Time, data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	day := now.UTC().Format("2006-01-02")
	if day != r.day {
		if err := r.rotateLocked(day); err != nil {
			return err
		}
	}
	if *file == nil {
		path := filepath.Join(r.dir, name+"-"+day+".jsonl")
		handle, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
		if err != nil {
			return fmt.Errorf("open %s record: %w", name, err)
		}
		if err := handle.Chmod(0o640); err != nil {
			_ = handle.Close()
			return fmt.Errorf("secure %s record: %w", name, err)
		}
		*file = handle
	}

	line := make([]byte, len(data)+1)
	copy(line, data)
	line[len(data)] = '\n'
	n, err := (*file).Write(line)
	if err != nil || n != len(line) {
		closeErr := (*file).Close()
		*file = nil
		if err == nil {
			err = io.ErrShortWrite
		}
		return errors.Join(fmt.Errorf("append %s record: %w", name, err), closeErr)
	}
	return nil
}

func (r *Recorder) rotateLocked(day string) error {
	err := r.closeLocked()
	r.day = day
	if err != nil {
		return fmt.Errorf("rotate record files: %w", err)
	}
	return nil
}

func (r *Recorder) closeLocked() error {
	var errs []error
	for _, item := range []struct {
		name string
		file **os.File
	}{
		{name: "auctions", file: &r.auctions},
		{name: "notifications", file: &r.notifs},
	} {
		if *item.file == nil {
			continue
		}
		if err := (*item.file).Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %s record: %w", item.name, err))
		}
		*item.file = nil
	}
	return errors.Join(errs...)
}

func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	err := r.closeLocked()
	r.day = ""
	return err
}
