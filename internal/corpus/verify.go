package corpus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/contract"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/record"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

func VerifyAndReplay(ctx context.Context, options VerifyOptions) (Report, error) {
	if err := validateRuntime(options.Runtime); err != nil {
		return Report{}, err
	}
	limits, err := options.Limits.normalized()
	if err != nil {
		return Report{}, err
	}
	if options.CorpusDir == "" {
		return Report{}, errors.New("corpus directory is empty")
	}
	directory, err := secureDirectory(options.CorpusDir)
	if err != nil {
		return Report{}, err
	}
	if err := verifyInventory(directory); err != nil {
		return Report{}, err
	}

	manifestData, _, err := regularFile(filepath.Join(directory, ManifestFile), limits.MaxCorpusBytes)
	if err != nil {
		return Report{}, err
	}
	var manifest Manifest
	if err := decodeStrict(manifestData, &manifest); err != nil {
		return Report{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := validateManifest(manifest, options.Runtime, limits); err != nil {
		return Report{}, err
	}
	entriesPath := filepath.Join(directory, manifest.Entries.Path)
	entriesData, info, err := regularFile(entriesPath, limits.MaxCorpusBytes)
	if err != nil {
		return Report{}, err
	}
	if info.Size() != manifest.Entries.Bytes || int64(len(entriesData)) != manifest.Entries.Bytes {
		return Report{}, errors.New("entry file byte count differs from manifest")
	}
	if digestBytes(entriesData) != manifest.Entries.SHA256 {
		return Report{}, errors.New("entry file SHA-256 differs from manifest")
	}

	entries, err := decodeEntries(entriesData, manifest, limits)
	if err != nil {
		return Report{}, err
	}
	results := make([]Result, 0, len(entries))
	config := manifest.Config.SolveConfig()
	var aggregate strings.Builder
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		actual := normalizeExpected(solve.Solve(ctx, &entry.Auction, config))
		if !reflect.DeepEqual(actual, entry.Expected) {
			return Report{}, fmt.Errorf("entry %s replay output differs from committed expectation", entry.ID)
		}
		outputData, err := canonicalJSON(actual)
		if err != nil {
			return Report{}, err
		}
		outputDigest := digestBytes(outputData)
		results = append(results, Result{EntryID: entry.ID, OutputSHA256: outputDigest})
		aggregate.WriteString(entry.ID)
		aggregate.WriteByte(':')
		aggregate.WriteString(outputDigest)
		aggregate.WriteByte('\n')
	}

	return Report{
		Schema:          ReportSchema,
		ManifestSHA256:  digestBytes(manifestData),
		EngineCommit:    manifest.EngineCommit,
		Toolchain:       manifest.Toolchain,
		UpstreamCommit:  manifest.UpstreamCommit,
		ConfigSHA256:    manifest.ConfigSHA256,
		EntryFileSHA256: manifest.Entries.SHA256,
		Entries:         len(entries),
		Results:         results,
		AggregateSHA256: digestBytes([]byte(aggregate.String())),
	}, nil
}

func validateManifest(manifest Manifest, runtimeIdentity RuntimeIdentity, limits Limits) error {
	if manifest.Schema != ManifestSchema {
		return fmt.Errorf("unexpected manifest schema %q", manifest.Schema)
	}
	if manifest.EngineCommit != runtimeIdentity.EngineCommit || manifest.Toolchain != runtimeIdentity.Toolchain || manifest.UpstreamCommit != runtimeIdentity.UpstreamCommit {
		return errors.New("manifest source, toolchain or upstream identity differs from the replay binary")
	}
	identity := record.RunIdentity{
		Schema:         record.RunIdentitySchema,
		EngineCommit:   manifest.EngineCommit,
		Toolchain:      manifest.Toolchain,
		UpstreamCommit: manifest.UpstreamCommit,
		Config:         manifest.Config,
		ConfigSHA256:   manifest.ConfigSHA256,
	}
	if err := identity.Validate(); err != nil {
		return fmt.Errorf("manifest run identity: %w", err)
	}
	if err := safeBaseName(manifest.Entries.Path); err != nil {
		return err
	}
	if manifest.Entries.Path != EntriesFile {
		return fmt.Errorf("manifest entry path must be %q", EntriesFile)
	}
	if len(manifest.Entries.SHA256) != 64 {
		return errors.New("manifest entry SHA-256 is malformed")
	}
	if manifest.Entries.Bytes < 1 || manifest.Entries.Bytes > limits.MaxCorpusBytes {
		return errors.New("manifest entry byte count is outside limits")
	}
	if manifest.Entries.Records < 1 || manifest.Entries.Records > limits.MaxRecords {
		return errors.New("manifest entry record count is outside limits")
	}
	if manifest.Redactions == nil {
		return errors.New("manifest redactions must be an explicit array")
	}
	allowedRedactions := map[string]struct{}{"auction.orders[].signature": {}}
	seen := map[string]struct{}{}
	for _, redaction := range manifest.Redactions {
		if _, ok := allowedRedactions[redaction]; !ok {
			return fmt.Errorf("unknown redaction rule %q", redaction)
		}
		if _, duplicate := seen[redaction]; duplicate {
			return fmt.Errorf("duplicate redaction rule %q", redaction)
		}
		seen[redaction] = struct{}{}
	}
	return nil
}

func decodeEntries(data []byte, manifest Manifest, limits Limits) ([]Entry, error) {
	scanner := scannerForBytes(data, limits.MaxLineBytes)
	entries := make([]Entry, 0, manifest.Entries.Records)
	seen := map[string]struct{}{}
	previous := ""
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.Clone(scanner.Bytes())
		if len(bytes.TrimSpace(line)) == 0 {
			return nil, fmt.Errorf("entries:%d: blank JSONL line", lineNumber)
		}
		var entry Entry
		if err := decodeStrict(line, &entry); err != nil {
			return nil, fmt.Errorf("entries:%d: %w", lineNumber, err)
		}
		if entry.Schema != EntrySchema {
			return nil, fmt.Errorf("entries:%d: unexpected entry schema %q", lineNumber, entry.Schema)
		}
		if entry.ID == "" || len(entry.ID) != 64 {
			return nil, fmt.Errorf("entries:%d: malformed entry ID", lineNumber)
		}
		if previous != "" && entry.ID <= previous {
			return nil, fmt.Errorf("entries:%d: entries are not strictly sorted", lineNumber)
		}
		previous = entry.ID
		if _, duplicate := seen[entry.ID]; duplicate {
			return nil, fmt.Errorf("entries:%d: duplicate entry ID", lineNumber)
		}
		seen[entry.ID] = struct{}{}
		auctionData, err := json.Marshal(entry.Auction)
		if err != nil {
			return nil, err
		}
		if err := contract.ValidateAuctionJSON(auctionData); err != nil {
			return nil, fmt.Errorf("entries:%d: auction violates pinned contract: %w", lineNumber, err)
		}
		auctionID := ""
		if entry.Auction.ID != nil {
			auctionID = *entry.Auction.ID
		}
		if entry.AuctionID != auctionID {
			return nil, fmt.Errorf("entries:%d: auctionId differs from payload", lineNumber)
		}
		expectedID, err := entryID(entry.Auction, manifest.ConfigSHA256)
		if err != nil {
			return nil, err
		}
		if entry.ID != expectedID {
			return nil, fmt.Errorf("entries:%d: entry ID does not bind auction and config", lineNumber)
		}
		entry.Expected = normalizeExpected(solve.Result{Solutions: entry.Expected.Solutions, Stats: entry.Expected.Stats})
		entries = append(entries, entry)
		if len(entries) > limits.MaxRecords {
			return nil, errors.New("entry count exceeds limit")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan entries: %w", err)
	}
	if len(entries) != manifest.Entries.Records {
		return nil, fmt.Errorf("entry count differs from manifest: got %d want %d", len(entries), manifest.Entries.Records)
	}
	return entries, nil
}

func verifyInventory(directory string) error {
	items, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	actual := make([]string, 0, len(items))
	for _, item := range items {
		if item.Type()&os.ModeSymlink != 0 || item.IsDir() {
			return fmt.Errorf("corpus contains non-regular entry %q", item.Name())
		}
		actual = append(actual, item.Name())
	}
	sort.Strings(actual)
	expected := []string{EntriesFile, ManifestFile}
	sort.Strings(expected)
	if !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("corpus inventory differs: got %v want %v", actual, expected)
	}
	return nil
}
