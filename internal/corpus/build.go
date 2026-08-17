package corpus

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/contract"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/record"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

func Build(ctx context.Context, options BuildOptions) (Manifest, error) {
	if err := validateRuntime(options.Runtime); err != nil {
		return Manifest{}, err
	}
	limits, err := options.Limits.normalized()
	if err != nil {
		return Manifest{}, err
	}
	if len(options.Inputs) == 0 {
		return Manifest{}, errors.New("at least one input JSONL file is required")
	}
	if options.OutputDir == "" {
		return Manifest{}, errors.New("output directory is empty")
	}

	entries, identity, err := readSourceRecords(ctx, options.Inputs, options.Runtime, limits, options.RedactSignatures)
	if err != nil {
		return Manifest{}, err
	}
	if len(entries) == 0 {
		return Manifest{}, errors.New("input contains no replayable auction records")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	absoluteOutput, err := filepath.Abs(options.OutputDir)
	if err != nil {
		return Manifest{}, err
	}
	if _, err := os.Lstat(absoluteOutput); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return Manifest{}, fmt.Errorf("output already exists: %s", absoluteOutput)
		}
		return Manifest{}, err
	}
	parent, err := secureDirectory(filepath.Dir(absoluteOutput))
	if err != nil {
		return Manifest{}, err
	}
	temporary, err := os.MkdirTemp(parent, ".aladdin-corpus-*.tmp")
	if err != nil {
		return Manifest{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := os.Chmod(temporary, 0o750); err != nil {
		return Manifest{}, err
	}

	entriesData, err := encodeEntries(entries)
	if err != nil {
		return Manifest{}, err
	}
	if int64(len(entriesData)) > limits.MaxCorpusBytes {
		return Manifest{}, fmt.Errorf("canonical entry file exceeds byte limit: %d > %d", len(entriesData), limits.MaxCorpusBytes)
	}
	entriesPath := filepath.Join(temporary, EntriesFile)
	if err := writeExclusive(entriesPath, entriesData, 0o640); err != nil {
		return Manifest{}, fmt.Errorf("write entries: %w", err)
	}

	redactions := []string{}
	if options.RedactSignatures {
		redactions = append(redactions, "auction.orders[].signature")
	}
	manifest := Manifest{
		Schema:         ManifestSchema,
		EngineCommit:   identity.EngineCommit,
		Toolchain:      identity.Toolchain,
		UpstreamCommit: identity.UpstreamCommit,
		Config:         identity.Config,
		ConfigSHA256:   identity.ConfigSHA256,
		Redactions:     redactions,
		Entries: FileDescriptor{
			Path:    EntriesFile,
			SHA256:  digestBytes(entriesData),
			Bytes:   int64(len(entriesData)),
			Records: len(entries),
		},
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	manifestData = append(manifestData, '\n')
	if err := writeExclusive(filepath.Join(temporary, ManifestFile), manifestData, 0o640); err != nil {
		return Manifest{}, fmt.Errorf("write manifest: %w", err)
	}
	if err := syncDirectory(temporary); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(temporary, absoluteOutput); err != nil {
		return Manifest{}, fmt.Errorf("publish corpus: %w", err)
	}
	published = true
	if err := syncDirectory(parent); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func readSourceRecords(
	ctx context.Context,
	inputs []string,
	runtimeIdentity RuntimeIdentity,
	limits Limits,
	redactSignatures bool,
) ([]Entry, record.RunIdentity, error) {
	paths := append([]string(nil), inputs...)
	sort.Strings(paths)
	seenPaths := map[string]struct{}{}
	seenEntries := map[string]struct{}{}
	seenAuctionIDs := map[string]string{}
	var expectedIdentity record.RunIdentity
	var entries []Entry
	var totalBytes int64

	for _, input := range paths {
		if err := ctx.Err(); err != nil {
			return nil, record.RunIdentity{}, err
		}
		absolute, err := filepath.Abs(input)
		if err != nil {
			return nil, record.RunIdentity{}, err
		}
		if _, duplicate := seenPaths[absolute]; duplicate {
			return nil, record.RunIdentity{}, fmt.Errorf("duplicate input path: %s", absolute)
		}
		seenPaths[absolute] = struct{}{}
		file, info, err := openRegular(absolute, limits.MaxInputBytes)
		if err != nil {
			return nil, record.RunIdentity{}, err
		}
		totalBytes += info.Size()
		if totalBytes > limits.MaxInputBytes {
			_ = file.Close()
			return nil, record.RunIdentity{}, fmt.Errorf("combined input exceeds byte limit: %d > %d", totalBytes, limits.MaxInputBytes)
		}
		scanner := scannerFor(file, limits.MaxLineBytes)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return nil, record.RunIdentity{}, err
			}
			line := bytes.Clone(scanner.Bytes())
			if len(bytes.TrimSpace(line)) == 0 {
				_ = file.Close()
				return nil, record.RunIdentity{}, fmt.Errorf("%s:%d: blank JSONL line", absolute, lineNumber)
			}
			var source record.AuctionRecord
			if err := decodeStrict(line, &source); err != nil {
				_ = file.Close()
				return nil, record.RunIdentity{}, fmt.Errorf("%s:%d: decode auction record: %w", absolute, lineNumber, err)
			}
			entry, identity, err := entryFromSource(source, runtimeIdentity, redactSignatures)
			if err != nil {
				_ = file.Close()
				return nil, record.RunIdentity{}, fmt.Errorf("%s:%d: %w", absolute, lineNumber, err)
			}
			if len(entries) == 0 {
				expectedIdentity = identity
			} else if !expectedIdentity.Equal(identity) {
				_ = file.Close()
				return nil, record.RunIdentity{}, fmt.Errorf("%s:%d: run identity differs from earlier records", absolute, lineNumber)
			}
			if _, duplicate := seenEntries[entry.ID]; duplicate {
				_ = file.Close()
				return nil, record.RunIdentity{}, fmt.Errorf("%s:%d: duplicate replay entry %s", absolute, lineNumber, entry.ID)
			}
			seenEntries[entry.ID] = struct{}{}
			if entry.AuctionID != "" {
				if prior, duplicate := seenAuctionIDs[entry.AuctionID]; duplicate && prior != entry.ID {
					_ = file.Close()
					return nil, record.RunIdentity{}, fmt.Errorf("%s:%d: auction ID %q binds to multiple inputs", absolute, lineNumber, entry.AuctionID)
				}
				seenAuctionIDs[entry.AuctionID] = entry.ID
			}
			entries = append(entries, entry)
			if len(entries) > limits.MaxRecords {
				_ = file.Close()
				return nil, record.RunIdentity{}, fmt.Errorf("record count exceeds limit: %d > %d", len(entries), limits.MaxRecords)
			}
		}
		scanErr := scanner.Err()
		closeErr := file.Close()
		if err := errors.Join(scanErr, closeErr); err != nil {
			return nil, record.RunIdentity{}, fmt.Errorf("read %s: %w", absolute, err)
		}
	}
	return entries, expectedIdentity, nil
}

func entryFromSource(source record.AuctionRecord, runtimeIdentity RuntimeIdentity, redactSignatures bool) (Entry, record.RunIdentity, error) {
	if source.Schema != record.ReplayableAuctionRecordSchema {
		return Entry{}, record.RunIdentity{}, fmt.Errorf("record schema %q is not replayable", source.Schema)
	}
	if source.Run == nil {
		return Entry{}, record.RunIdentity{}, errors.New("replayable record is missing run identity")
	}
	identity := *source.Run
	if err := identity.Validate(); err != nil {
		return Entry{}, record.RunIdentity{}, fmt.Errorf("invalid run identity: %w", err)
	}
	if identity.EngineCommit != runtimeIdentity.EngineCommit || identity.Toolchain != runtimeIdentity.Toolchain || identity.UpstreamCommit != runtimeIdentity.UpstreamCommit {
		return Entry{}, record.RunIdentity{}, errors.New("record source, toolchain or upstream identity differs from the replay binary")
	}
	if source.Auction == nil {
		return Entry{}, record.RunIdentity{}, errors.New("replayable record is missing the full auction")
	}
	if source.ElapsedMs < 0 {
		return Entry{}, record.RunIdentity{}, errors.New("negative solve latency")
	}
	if _, err := time.Parse(time.RFC3339Nano, source.Timestamp); err != nil {
		return Entry{}, record.RunIdentity{}, fmt.Errorf("invalid record timestamp: %w", err)
	}

	auction, err := cloneAuction(source.Auction)
	if err != nil {
		return Entry{}, record.RunIdentity{}, err
	}
	if redactSignatures {
		for index := range auction.Orders {
			auction.Orders[index].Signature = "0x"
		}
	}
	auctionData, err := json.Marshal(auction)
	if err != nil {
		return Entry{}, record.RunIdentity{}, err
	}
	if err := contract.ValidateAuctionJSON(auctionData); err != nil {
		return Entry{}, record.RunIdentity{}, fmt.Errorf("auction violates pinned contract: %w", err)
	}
	auctionID := ""
	if auction.ID != nil {
		auctionID = *auction.ID
	}
	if source.AuctionID != auctionID {
		return Entry{}, record.RunIdentity{}, fmt.Errorf("record auctionId %q differs from payload ID %q", source.AuctionID, auctionID)
	}

	actual := normalizeExpected(solve.Solve(context.Background(), &auction, identity.Config.SolveConfig()))
	recorded := normalizeExpected(solve.Result{Solutions: source.Solutions, Stats: source.Stats})
	if !reflect.DeepEqual(actual, recorded) {
		return Entry{}, record.RunIdentity{}, errors.New("recorded solutions or stats do not reproduce under the recorded configuration")
	}
	entryID, err := entryID(auction, identity.ConfigSHA256)
	if err != nil {
		return Entry{}, record.RunIdentity{}, err
	}
	return Entry{
		Schema:    EntrySchema,
		ID:        entryID,
		AuctionID: auctionID,
		Auction:   auction,
		Expected:  actual,
	}, identity, nil
}

func normalizeExpected(result solve.Result) Expected {
	if result.Solutions == nil {
		result.Solutions = []api.Solution{}
	}
	if result.Stats.PoolsSkipped == nil {
		result.Stats.PoolsSkipped = map[string]int{}
	}
	return Expected{Solutions: result.Solutions, Stats: result.Stats}
}

func cloneAuction(source *api.Auction) (api.Auction, error) {
	data, err := json.Marshal(source)
	if err != nil {
		return api.Auction{}, err
	}
	var clone api.Auction
	if err := json.Unmarshal(data, &clone); err != nil {
		return api.Auction{}, err
	}
	return clone, nil
}

func entryID(auction api.Auction, configSHA256 string) (string, error) {
	data, err := canonicalJSON(auction)
	if err != nil {
		return "", err
	}
	payload := make([]byte, 0, len(data)+1+len(configSHA256))
	payload = append(payload, data...)
	payload = append(payload, '\n')
	payload = append(payload, configSHA256...)
	return digestBytes(payload), nil
}

func encodeEntries(entries []Entry) ([]byte, error) {
	var buffer bytes.Buffer
	writer := bufio.NewWriter(&buffer)
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write(data); err != nil {
			return nil, err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return nil, err
		}
	}
	if err := writer.Flush(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	return errors.Join(syncErr, closeErr)
}

var _ io.Reader
var _ = strings.Builder{}
