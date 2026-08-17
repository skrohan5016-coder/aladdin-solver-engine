package corpus

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

func readSealedSourceRecords(
	ctx context.Context,
	inputs []string,
	runtimeIdentity RuntimeIdentity,
	limits Limits,
	redactSignatures bool,
) ([]Entry, recordIdentity, []SourceDescriptor, error) {
	paths := append([]string(nil), inputs...)
	sort.Strings(paths)
	seenPaths := map[string]struct{}{}
	seenEntries := map[string]struct{}{}
	seenAuctionIDs := map[string]string{}
	var expected recordIdentity
	var entries []Entry
	var sources []SourceDescriptor
	var totalBytes int64

	for _, input := range paths {
		if err := ctx.Err(); err != nil {
			return nil, recordIdentity{}, nil, err
		}
		absolute, err := filepath.Abs(input)
		if err != nil {
			return nil, recordIdentity{}, nil, err
		}
		if _, duplicate := seenPaths[absolute]; duplicate {
			return nil, recordIdentity{}, nil, fmt.Errorf("duplicate input path: %s", absolute)
		}
		seenPaths[absolute] = struct{}{}

		file, before, err := openRegular(absolute, limits.MaxInputBytes)
		if err != nil {
			return nil, recordIdentity{}, nil, err
		}
		totalBytes += before.Size()
		if totalBytes > limits.MaxInputBytes {
			_ = file.Close()
			return nil, recordIdentity{}, nil, fmt.Errorf("combined input exceeds byte limit: %d > %d", totalBytes, limits.MaxInputBytes)
		}
		if before.Size() == 0 {
			_ = file.Close()
			return nil, recordIdentity{}, nil, fmt.Errorf("empty recorder input: %s", absolute)
		}
		last := []byte{0}
		if _, err := file.ReadAt(last, before.Size()-1); err != nil {
			_ = file.Close()
			return nil, recordIdentity{}, nil, fmt.Errorf("read final source byte: %w", err)
		}
		if last[0] != '\n' {
			_ = file.Close()
			return nil, recordIdentity{}, nil, fmt.Errorf("recorder input has an unterminated final JSONL record: %s", absolute)
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			_ = file.Close()
			return nil, recordIdentity{}, nil, err
		}

		hasher := sha256.New()
		scanner := bufio.NewScanner(io.TeeReader(file, hasher))
		initial := 64 << 10
		if limits.MaxLineBytes < initial {
			initial = limits.MaxLineBytes
		}
		scanner.Buffer(make([]byte, initial), limits.MaxLineBytes)
		lineNumber := 0
		recordsBefore := len(entries)
		for scanner.Scan() {
			lineNumber++
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return nil, recordIdentity{}, nil, err
			}
			line := bytes.Clone(scanner.Bytes())
			if len(bytes.TrimSpace(line)) == 0 {
				_ = file.Close()
				return nil, recordIdentity{}, nil, fmt.Errorf("%s:%d: blank JSONL line", absolute, lineNumber)
			}
			source, err := decodeSourceRecord(line)
			if err != nil {
				_ = file.Close()
				return nil, recordIdentity{}, nil, fmt.Errorf("%s:%d: %w", absolute, lineNumber, err)
			}
			entry, identity, err := entryFromSource(ctx, source, runtimeIdentity, redactSignatures)
			if err != nil {
				_ = file.Close()
				return nil, recordIdentity{}, nil, fmt.Errorf("%s:%d: %w", absolute, lineNumber, err)
			}
			if len(entries) == 0 {
				expected = recordIdentity{RunIdentity: identity}
			} else if !expected.RunIdentity.Equal(identity) {
				_ = file.Close()
				return nil, recordIdentity{}, nil, fmt.Errorf("%s:%d: run identity differs from earlier records", absolute, lineNumber)
			}
			if _, duplicate := seenEntries[entry.ID]; duplicate {
				_ = file.Close()
				return nil, recordIdentity{}, nil, fmt.Errorf("%s:%d: duplicate replay entry %s", absolute, lineNumber, entry.ID)
			}
			seenEntries[entry.ID] = struct{}{}
			if entry.AuctionID != "" {
				if prior, duplicate := seenAuctionIDs[entry.AuctionID]; duplicate && prior != entry.ID {
					_ = file.Close()
					return nil, recordIdentity{}, nil, fmt.Errorf("%s:%d: auction ID %q binds to multiple inputs", absolute, lineNumber, entry.AuctionID)
				}
				seenAuctionIDs[entry.AuctionID] = entry.ID
			}
			entries = append(entries, entry)
			if len(entries) > limits.MaxRecords {
				_ = file.Close()
				return nil, recordIdentity{}, nil, fmt.Errorf("record count exceeds limit: %d > %d", len(entries), limits.MaxRecords)
			}
		}
		scanErr := scanner.Err()
		after, statErr := file.Stat()
		closeErr := file.Close()
		if err := errors.Join(scanErr, statErr, closeErr); err != nil {
			return nil, recordIdentity{}, nil, fmt.Errorf("read %s: %w", absolute, err)
		}
		if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return nil, recordIdentity{}, nil, fmt.Errorf("recorder input changed while being read: %s", absolute)
		}
		recordCount := len(entries) - recordsBefore
		if recordCount == 0 {
			return nil, recordIdentity{}, nil, fmt.Errorf("recorder input contains no records: %s", absolute)
		}
		sources = append(sources, SourceDescriptor{
			SHA256:  hex.EncodeToString(hasher.Sum(nil)),
			Bytes:   before.Size(),
			Records: recordCount,
		})
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].SHA256 != sources[j].SHA256 {
			return sources[i].SHA256 < sources[j].SHA256
		}
		if sources[i].Bytes != sources[j].Bytes {
			return sources[i].Bytes < sources[j].Bytes
		}
		return sources[i].Records < sources[j].Records
	})
	return entries, expected, sources, nil
}
