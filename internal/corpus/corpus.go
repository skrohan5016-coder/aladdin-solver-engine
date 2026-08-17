// Package corpus publishes and verifies immutable offline solver replay corpora.
package corpus

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/buildinfo"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/contract"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/record"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

const (
	ManifestSchema = "aladdin-offline-replay-corpus/v1"
	ExpectedSchema = "aladdin-offline-replay-expected/v1"
	ReportSchema   = "aladdin-offline-replay-report/v1"

	defaultMaxRecordBytes = int64(64 << 20)
	defaultMaxFileBytes   = int64(64 << 20)
	defaultMaxCases       = 100_000
)

type RedactionPolicy string

const (
	RedactNone       RedactionPolicy = "none"
	RedactSignatures RedactionPolicy = "signatures"
)

type Manifest struct {
	Schema          string               `json:"schema"`
	Engine          buildinfo.Identity   `json:"engine"`
	UpstreamCommit  string               `json:"upstreamCommit"`
	Config          solve.ConfigSnapshot `json:"config"`
	ConfigSHA256    string               `json:"configSha256"`
	RedactionPolicy RedactionPolicy      `json:"redactionPolicy"`
	Entries         []Entry              `json:"entries"`
}

type Entry struct {
	Name            string `json:"name"`
	AuctionID       string `json:"auctionId"`
	RecordTimestamp string `json:"recordTimestamp"`
	AuctionFile     string `json:"auctionFile"`
	AuctionSHA256   string `json:"auctionSha256"`
	AuctionBytes    int64  `json:"auctionBytes"`
	ExpectedFile    string `json:"expectedFile"`
	ExpectedSHA256  string `json:"expectedSha256"`
	ExpectedBytes   int64  `json:"expectedBytes"`
}

type Expected struct {
	Schema    string         `json:"schema"`
	Stats     solve.Stats    `json:"stats"`
	Solutions []api.Solution `json:"solutions"`
}

type Report struct {
	Schema            string `json:"schema"`
	CorpusSHA256      string `json:"corpusSha256"`
	SourceCommit      string `json:"sourceCommit"`
	RecordedCommit    string `json:"recordedCommit"`
	GoVersion         string `json:"goVersion"`
	RecordedGoVersion string `json:"recordedGoVersion"`
	ConfigSHA256      string `json:"configSha256"`
	Cases             int    `json:"cases"`
	ResultsSHA256     string `json:"resultsSha256"`
}

type PackOptions struct {
	SourceCommit     string
	RedactionPolicy  RedactionPolicy
	MaxRecordBytes   int64
	MaxCases         int
	RequireToolchain bool
}

type ReplayOptions struct {
	SourceCommit     string
	MaxFileBytes     int64
	MaxCases         int
	RequireToolchain bool
}

type preparedCase struct {
	recordTimestamp string
	auctionID       string
	auction         []byte
	expected        []byte
	auctionDigest   string
	expectedDigest  string
}

func Pack(ctx context.Context, recordPaths []string, outputDir string, options PackOptions) (Manifest, error) {
	options = resolvePackOptions(options)
	if !buildinfo.ValidCommit(options.SourceCommit) {
		return Manifest{}, fmt.Errorf("source commit %q is not an exact lowercase 40-hex SHA", options.SourceCommit)
	}
	if outputDir == "" {
		return Manifest{}, errors.New("output directory is empty")
	}
	paths := append([]string(nil), recordPaths...)
	sort.Strings(paths)
	if len(paths) == 0 {
		return Manifest{}, errors.New("no auction record files were supplied")
	}

	var identity *record.ReplayIdentity
	var cases []preparedCase
	for _, path := range paths {
		records, err := readAuctionRecords(path, options.MaxRecordBytes, options.MaxCases-len(cases))
		if err != nil {
			return Manifest{}, err
		}
		for index := range records {
			if err := ctx.Err(); err != nil {
				return Manifest{}, err
			}
			item := records[index]
			if item.Identity == nil || item.Auction == nil {
				return Manifest{}, fmt.Errorf("%s: record %d is missing replay identity or full auction", path, index+1)
			}
			if err := validateIdentity(*item.Identity, options.SourceCommit, options.RequireToolchain); err != nil {
				return Manifest{}, fmt.Errorf("%s: record %d: %w", path, index+1, err)
			}
			if identity == nil {
				copy := *item.Identity
				identity = &copy
			} else if !sameIdentity(*identity, *item.Identity) {
				return Manifest{}, fmt.Errorf("%s: record %d has a different engine, toolchain, upstream, or config identity", path, index+1)
			}
			prepared, err := prepareRecord(ctx, item, options.RedactionPolicy)
			if err != nil {
				return Manifest{}, fmt.Errorf("%s: record %d: %w", path, index+1, err)
			}
			cases = append(cases, prepared)
			if len(cases) > options.MaxCases {
				return Manifest{}, fmt.Errorf("corpus exceeds maximum case count %d", options.MaxCases)
			}
		}
	}
	if len(cases) == 0 || identity == nil {
		return Manifest{}, errors.New("no replayable full-auction records were found")
	}
	sort.Slice(cases, func(i, j int) bool {
		left := cases[i]
		right := cases[j]
		if left.auctionDigest != right.auctionDigest {
			return left.auctionDigest < right.auctionDigest
		}
		if left.expectedDigest != right.expectedDigest {
			return left.expectedDigest < right.expectedDigest
		}
		if left.auctionID != right.auctionID {
			return left.auctionID < right.auctionID
		}
		return left.recordTimestamp < right.recordTimestamp
	})
	seenCases := map[string]struct{}{}
	for _, item := range cases {
		key := item.auctionDigest + ":" + item.expectedDigest
		if _, duplicate := seenCases[key]; duplicate {
			return Manifest{}, fmt.Errorf("duplicate replay case %s", key)
		}
		seenCases[key] = struct{}{}
	}

	manifest := Manifest{
		Schema:          ManifestSchema,
		Engine:          identity.Engine,
		UpstreamCommit:  identity.UpstreamCommit,
		Config:          identity.Config,
		ConfigSHA256:    identity.ConfigSHA256,
		RedactionPolicy: options.RedactionPolicy,
		Entries:         make([]Entry, 0, len(cases)),
	}
	if err := publish(outputDir, &manifest, cases); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func Replay(ctx context.Context, corpusDir string, options ReplayOptions) (Report, error) {
	options = resolveReplayOptions(options)
	if !buildinfo.ValidCommit(options.SourceCommit) {
		return Report{}, fmt.Errorf("source commit %q is not an exact lowercase 40-hex SHA", options.SourceCommit)
	}
	manifestPath := filepath.Join(corpusDir, "manifest.json")
	manifestBytes, err := readRegularBounded(manifestPath, options.MaxFileBytes)
	if err != nil {
		return Report{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	if err := decodeStrict(manifestBytes, &manifest); err != nil {
		return Report{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := validateManifest(manifest, options); err != nil {
		return Report{}, err
	}
	if err := validateInventory(corpusDir, manifest); err != nil {
		return Report{}, err
	}
	config, err := manifest.Config.Config()
	if err != nil {
		return Report{}, fmt.Errorf("decode corpus config: %w", err)
	}

	resultHash := sha256.New()
	for index, entry := range manifest.Entries {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		auctionBytes, err := readEntry(corpusDir, entry.AuctionFile, entry.AuctionBytes, entry.AuctionSHA256, options.MaxFileBytes)
		if err != nil {
			return Report{}, fmt.Errorf("entry %d auction: %w", index, err)
		}
		expectedBytes, err := readEntry(corpusDir, entry.ExpectedFile, entry.ExpectedBytes, entry.ExpectedSHA256, options.MaxFileBytes)
		if err != nil {
			return Report{}, fmt.Errorf("entry %d expected: %w", index, err)
		}
		if err := contract.ValidateAuctionJSON(auctionBytes); err != nil {
			return Report{}, fmt.Errorf("entry %d auction contract: %w", index, err)
		}
		var auction api.Auction
		if err := decodeStrict(auctionBytes, &auction); err != nil {
			return Report{}, fmt.Errorf("entry %d auction decode: %w", index, err)
		}
		var expected Expected
		if err := decodeStrict(expectedBytes, &expected); err != nil {
			return Report{}, fmt.Errorf("entry %d expected decode: %w", index, err)
		}
		if expected.Schema != ExpectedSchema {
			return Report{}, fmt.Errorf("entry %d has unsupported expected schema %q", index, expected.Schema)
		}
		result := solve.Solve(ctx, &auction, config)
		actual := normalizeExpected(Expected{
			Schema:    ExpectedSchema,
			Stats:     result.Stats,
			Solutions: result.Solutions,
		})
		actualBytes, err := canonicalJSON(actual)
		if err != nil {
			return Report{}, fmt.Errorf("entry %d actual encode: %w", index, err)
		}
		canonicalExpected, err := canonicalJSON(normalizeExpected(expected))
		if err != nil {
			return Report{}, fmt.Errorf("entry %d expected normalize: %w", index, err)
		}
		if !bytes.Equal(actualBytes, canonicalExpected) {
			return Report{}, fmt.Errorf("entry %d replay output differs from sealed expected evidence", index)
		}
		resultHash.Write([]byte(entry.Name))
		resultHash.Write([]byte{0})
		resultHash.Write(actualBytes)
	}

	return Report{
		Schema:            ReportSchema,
		CorpusSHA256:      digest(manifestBytes),
		SourceCommit:      options.SourceCommit,
		RecordedCommit:    manifest.Engine.Commit,
		GoVersion:         runtime.Version(),
		RecordedGoVersion: manifest.Engine.GoVersion,
		ConfigSHA256:      manifest.ConfigSHA256,
		Cases:             len(manifest.Entries),
		ResultsSHA256:     fmt.Sprintf("%x", resultHash.Sum(nil)),
	}, nil
}

func ReportJSON(report Report) ([]byte, error) {
	return canonicalJSON(report)
}

func resolvePackOptions(options PackOptions) PackOptions {
	if options.SourceCommit == "" {
		options.SourceCommit = buildinfo.Commit
	}
	if options.RedactionPolicy == "" {
		options.RedactionPolicy = RedactSignatures
	}
	if options.MaxRecordBytes <= 0 {
		options.MaxRecordBytes = defaultMaxRecordBytes
	}
	if options.MaxCases <= 0 {
		options.MaxCases = defaultMaxCases
	}
	if !options.RequireToolchain {
		options.RequireToolchain = true
	}
	return options
}

func resolveReplayOptions(options ReplayOptions) ReplayOptions {
	if options.SourceCommit == "" {
		options.SourceCommit = buildinfo.Commit
	}
	if options.MaxFileBytes <= 0 {
		options.MaxFileBytes = defaultMaxFileBytes
	}
	if options.MaxCases <= 0 {
		options.MaxCases = defaultMaxCases
	}
	if !options.RequireToolchain {
		options.RequireToolchain = true
	}
	return options
}

func readAuctionRecords(path string, maxLineBytes int64, remaining int) ([]record.AuctionRecord, error) {
	if remaining <= 0 {
		return nil, errors.New("corpus case limit reached before reading all records")
	}
	file, err := openRegular(path)
	if err != nil {
		return nil, fmt.Errorf("open auction records %s: %w", path, err)
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64<<10)
	var records []record.AuctionRecord
	for lineNumber := 1; ; lineNumber++ {
		line, err := readCompleteLine(reader, maxLineBytes)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNumber, err)
		}
		if len(bytes.TrimSpace(line)) == 0 {
			return nil, fmt.Errorf("%s:%d: blank record line", path, lineNumber)
		}
		var item record.AuctionRecord
		if err := decodeStrict(bytes.TrimSuffix(line, []byte{'\n'}), &item); err != nil {
			return nil, fmt.Errorf("%s:%d: decode record: %w", path, lineNumber, err)
		}
		if item.Schema != record.AuctionRecordSchema {
			return nil, fmt.Errorf("%s:%d: unsupported auction record schema %q", path, lineNumber, item.Schema)
		}
		records = append(records, item)
		if len(records) > remaining {
			return nil, fmt.Errorf("%s: record count exceeds remaining case limit %d", path, remaining)
		}
	}
	return records, nil
}

func readCompleteLine(reader *bufio.Reader, maxBytes int64) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		line = append(line, fragment...)
		if int64(len(line)) > maxBytes {
			return nil, fmt.Errorf("record line exceeds %d bytes", maxBytes)
		}
		switch {
		case err == nil:
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) == 0:
			return nil, io.EOF
		case errors.Is(err, io.EOF):
			return nil, errors.New("partial final record without newline")
		default:
			return nil, err
		}
	}
}

func prepareRecord(ctx context.Context, item record.AuctionRecord, policy RedactionPolicy) (preparedCase, error) {
	if _, err := time.Parse(time.RFC3339Nano, item.Timestamp); err != nil {
		return preparedCase{}, fmt.Errorf("invalid record timestamp: %w", err)
	}
	config, err := item.Identity.Config.Config()
	if err != nil {
		return preparedCase{}, fmt.Errorf("decode record config: %w", err)
	}
	fresh := solve.Solve(ctx, item.Auction, config)
	sealed := normalizeExpected(Expected{Schema: ExpectedSchema, Stats: item.Stats, Solutions: item.Solutions})
	actual := normalizeExpected(Expected{Schema: ExpectedSchema, Stats: fresh.Stats, Solutions: fresh.Solutions})
	sealedBytes, err := canonicalJSON(sealed)
	if err != nil {
		return preparedCase{}, err
	}
	actualBytes, err := canonicalJSON(actual)
	if err != nil {
		return preparedCase{}, err
	}
	if !bytes.Equal(sealedBytes, actualBytes) {
		return preparedCase{}, errors.New("recorded solutions or stats do not reproduce under the recorded config")
	}
	auction, err := redactAuction(item.Auction, policy)
	if err != nil {
		return preparedCase{}, err
	}
	redacted := solve.Solve(ctx, auction, config)
	redactedBytes, err := canonicalJSON(normalizeExpected(Expected{Schema: ExpectedSchema, Stats: redacted.Stats, Solutions: redacted.Solutions}))
	if err != nil {
		return preparedCase{}, err
	}
	if !bytes.Equal(redactedBytes, actualBytes) {
		return preparedCase{}, errors.New("redaction changed solver output")
	}
	auctionBytes, err := canonicalJSON(auction)
	if err != nil {
		return preparedCase{}, err
	}
	if err := contract.ValidateAuctionJSON(auctionBytes); err != nil {
		return preparedCase{}, fmt.Errorf("redacted auction violates pinned contract: %w", err)
	}
	return preparedCase{
		recordTimestamp: item.Timestamp,
		auctionID:       item.AuctionID,
		auction:         auctionBytes,
		expected:        actualBytes,
		auctionDigest:   digest(auctionBytes),
		expectedDigest:  digest(actualBytes),
	}, nil
}

func redactAuction(auction *api.Auction, policy RedactionPolicy) (*api.Auction, error) {
	if auction == nil {
		return nil, errors.New("auction is nil")
	}
	data, err := json.Marshal(auction)
	if err != nil {
		return nil, err
	}
	var copied api.Auction
	if err := json.Unmarshal(data, &copied); err != nil {
		return nil, err
	}
	switch policy {
	case RedactNone:
	case RedactSignatures:
		for index := range copied.Orders {
			copied.Orders[index].Signature = "0x"
		}
	default:
		return nil, fmt.Errorf("unsupported redaction policy %q", policy)
	}
	return &copied, nil
}

func publish(outputDir string, manifest *Manifest, cases []preparedCase) error {
	if err := rejectSymlinkParents(outputDir); err != nil {
		return err
	}
	if _, err := os.Lstat(outputDir); err == nil {
		return fmt.Errorf("output directory already exists: %s", outputDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(outputDir)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return err
	}
	lockPath := outputDir + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("acquire corpus publication lock: %w", err)
	}
	_ = lock.Close()
	defer os.Remove(lockPath)

	temporary, err := os.MkdirTemp(parent, "."+filepath.Base(outputDir)+".partial-")
	if err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o700); err != nil {
		_ = os.RemoveAll(temporary)
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(temporary)
		}
	}()

	for index, item := range cases {
		name := fmt.Sprintf("case-%06d", index+1)
		auctionFile := name + ".auction.json"
		expectedFile := name + ".expected.json"
		if err := writeExclusive(filepath.Join(temporary, auctionFile), item.auction); err != nil {
			return err
		}
		if err := writeExclusive(filepath.Join(temporary, expectedFile), item.expected); err != nil {
			return err
		}
		manifest.Entries = append(manifest.Entries, Entry{
			Name:            name,
			AuctionID:       item.auctionID,
			RecordTimestamp: item.recordTimestamp,
			AuctionFile:     auctionFile,
			AuctionSHA256:   item.auctionDigest,
			AuctionBytes:    int64(len(item.auction)),
			ExpectedFile:    expectedFile,
			ExpectedSHA256:  item.expectedDigest,
			ExpectedBytes:   int64(len(item.expected)),
		})
	}
	manifestBytes, err := canonicalJSON(manifest)
	if err != nil {
		return err
	}
	if err := writeExclusive(filepath.Join(temporary, "manifest.json"), manifestBytes); err != nil {
		return err
	}
	if err := syncDirectory(temporary); err != nil {
		return err
	}
	if _, err := os.Lstat(outputDir); err == nil {
		return fmt.Errorf("output directory appeared during publication: %s", outputDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temporary, outputDir); err != nil {
		return fmt.Errorf("publish corpus: %w", err)
	}
	published = true
	return syncDirectory(parent)
}

func validateManifest(manifest Manifest, options ReplayOptions) error {
	if manifest.Schema != ManifestSchema {
		return fmt.Errorf("unsupported corpus schema %q", manifest.Schema)
	}
	if len(manifest.Entries) == 0 || len(manifest.Entries) > options.MaxCases {
		return fmt.Errorf("corpus case count %d is outside 1..%d", len(manifest.Entries), options.MaxCases)
	}
	identity := record.ReplayIdentity{
		Engine:         manifest.Engine,
		UpstreamCommit: manifest.UpstreamCommit,
		Config:         manifest.Config,
		ConfigSHA256:   manifest.ConfigSHA256,
	}
	if err := validateIdentity(identity, options.SourceCommit, options.RequireToolchain); err != nil {
		return err
	}
	if manifest.RedactionPolicy != RedactNone && manifest.RedactionPolicy != RedactSignatures {
		return fmt.Errorf("unsupported redaction policy %q", manifest.RedactionPolicy)
	}
	seenNames := map[string]struct{}{}
	seenFiles := map[string]struct{}{}
	for index, entry := range manifest.Entries {
		if entry.Name != fmt.Sprintf("case-%06d", index+1) {
			return fmt.Errorf("entry %d has non-canonical name %q", index, entry.Name)
		}
		if _, duplicate := seenNames[entry.Name]; duplicate {
			return fmt.Errorf("duplicate entry name %q", entry.Name)
		}
		seenNames[entry.Name] = struct{}{}
		if entry.AuctionFile != entry.Name+".auction.json" || entry.ExpectedFile != entry.Name+".expected.json" {
			return fmt.Errorf("entry %d file names are not canonical", index)
		}
		for _, file := range []string{entry.AuctionFile, entry.ExpectedFile} {
			if !safeFileName(file) {
				return fmt.Errorf("entry %d has unsafe file name %q", index, file)
			}
			if _, duplicate := seenFiles[file]; duplicate {
				return fmt.Errorf("duplicate corpus file %q", file)
			}
			seenFiles[file] = struct{}{}
		}
		if entry.AuctionBytes <= 0 || entry.ExpectedBytes <= 0 || entry.AuctionBytes > options.MaxFileBytes || entry.ExpectedBytes > options.MaxFileBytes {
			return fmt.Errorf("entry %d has invalid byte bounds", index)
		}
		if !validDigest(entry.AuctionSHA256) || !validDigest(entry.ExpectedSHA256) {
			return fmt.Errorf("entry %d has invalid SHA-256", index)
		}
	}
	return nil
}

func validateIdentity(identity record.ReplayIdentity, sourceCommit string, requireToolchain bool) error {
	if !buildinfo.ValidCommit(identity.Engine.Commit) {
		return fmt.Errorf("recorded engine commit %q is not exact", identity.Engine.Commit)
	}
	if identity.Engine.GoVersion == "" || identity.Engine.GOOS == "" || identity.Engine.GOARCH == "" {
		return errors.New("recorded toolchain identity is incomplete")
	}
	if identity.Engine.Commit != sourceCommit {
		return fmt.Errorf("source commit mismatch: recorded %s current %s", identity.Engine.Commit, sourceCommit)
	}
	if identity.UpstreamCommit != contract.UpstreamCommit {
		return fmt.Errorf("upstream commit mismatch: recorded %s current %s", identity.UpstreamCommit, contract.UpstreamCommit)
	}
	config, err := identity.Config.Config()
	if err != nil {
		return fmt.Errorf("recorded config: %w", err)
	}
	_, digestValue, err := solve.SnapshotConfig(config)
	if err != nil {
		return fmt.Errorf("recorded config: %w", err)
	}
	if digestValue != identity.ConfigSHA256 {
		return errors.New("recorded config digest mismatch")
	}
	if requireToolchain && identity.Engine.GoVersion != runtime.Version() {
		return fmt.Errorf("Go toolchain mismatch: recorded %s current %s", identity.Engine.GoVersion, runtime.Version())
	}
	return nil
}

func sameIdentity(left, right record.ReplayIdentity) bool {
	return left.Engine == right.Engine && left.UpstreamCommit == right.UpstreamCommit && left.Config == right.Config && left.ConfigSHA256 == right.ConfigSHA256
}

func normalizeExpected(value Expected) Expected {
	if value.Solutions == nil {
		value.Solutions = []api.Solution{}
	}
	if value.Stats.PoolsSkipped == nil {
		value.Stats.PoolsSkipped = map[string]int{}
	}
	return value
}

func canonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return contract.NormalizeJSON(encoded)
}

func decodeStrict(data []byte, target any) error {
	if err := contract.ValidateUniqueJSON(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple top-level JSON values")
		}
		return err
	}
	return nil
}

func openRegular(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !os.SameFile(before, after) || !after.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("file changed during open: %s", path)
	}
	return file, nil
}

func readRegularBounded(path string, maxBytes int64) ([]byte, error) {
	file, err := openRegular(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 || info.Size() > maxBytes {
		return nil, fmt.Errorf("file size %d is outside 1..%d", info.Size(), maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != info.Size() {
		return nil, errors.New("file size changed while reading")
	}
	return data, nil
}

func readEntry(dir, name string, expectedBytes int64, expectedDigest string, maxBytes int64) ([]byte, error) {
	if !safeFileName(name) {
		return nil, fmt.Errorf("unsafe file name %q", name)
	}
	data, err := readRegularBounded(filepath.Join(dir, name), maxBytes)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != expectedBytes {
		return nil, fmt.Errorf("byte length mismatch: got %d want %d", len(data), expectedBytes)
	}
	if digest(data) != expectedDigest {
		return nil, errors.New("SHA-256 mismatch")
	}
	return data, nil
}

func validateInventory(dir string, manifest Manifest) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	expected := map[string]struct{}{"manifest.json": {}}
	for _, entry := range manifest.Entries {
		expected[entry.AuctionFile] = struct{}{}
		expected[entry.ExpectedFile] = struct{}{}
	}
	actual := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("corpus inventory contains non-regular entry %q", entry.Name())
		}
		actual[entry.Name()] = struct{}{}
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("corpus inventory count mismatch: got %d want %d", len(actual), len(expected))
	}
	for name := range expected {
		if _, ok := actual[name]; !ok {
			return fmt.Errorf("corpus inventory is missing %q", name)
		}
	}
	for name := range actual {
		if _, ok := expected[name]; !ok {
			return fmt.Errorf("corpus inventory contains unlisted file %q", name)
		}
	}
	return nil
}

func writeExclusive(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	writeErr := func() error {
		if _, err := file.Write(data); err != nil {
			return err
		}
		return file.Sync()
	}()
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func rejectSymlinkParents(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	current := filepath.Dir(absolute)
	for {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinked output parent is not allowed: %s", current)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		next := filepath.Dir(current)
		if next == current {
			break
		}
		current = next
	}
	return nil
}

func safeFileName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`)
}

func digest(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for index := range value {
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}
