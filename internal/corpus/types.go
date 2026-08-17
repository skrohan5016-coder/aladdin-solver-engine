// Package corpus publishes and verifies deterministic offline replay corpora.
package corpus

import (
	"errors"
	"fmt"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/record"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

const (
	ManifestSchema = "aladdin-offline-replay-manifest/v1"
	EntrySchema    = "aladdin-offline-replay-entry/v1"
	ReportSchema   = "aladdin-offline-replay-report/v1"
	EntriesFile    = "entries.jsonl"
	ManifestFile   = "manifest.json"

	DefaultMaxInputBytes  int64 = 1 << 30
	DefaultMaxCorpusBytes int64 = 1 << 30
	DefaultMaxLineBytes         = 128 << 20
	DefaultMaxRecords           = 100_000
)

type RuntimeIdentity struct {
	EngineCommit   string
	Toolchain      string
	UpstreamCommit string
}

type Limits struct {
	MaxInputBytes  int64
	MaxCorpusBytes int64
	MaxLineBytes   int
	MaxRecords     int
}

func DefaultLimits() Limits {
	return Limits{
		MaxInputBytes:  DefaultMaxInputBytes,
		MaxCorpusBytes: DefaultMaxCorpusBytes,
		MaxLineBytes:   DefaultMaxLineBytes,
		MaxRecords:     DefaultMaxRecords,
	}
}

func (limits Limits) normalized() (Limits, error) {
	defaults := DefaultLimits()
	if limits.MaxInputBytes == 0 {
		limits.MaxInputBytes = defaults.MaxInputBytes
	}
	if limits.MaxCorpusBytes == 0 {
		limits.MaxCorpusBytes = defaults.MaxCorpusBytes
	}
	if limits.MaxLineBytes == 0 {
		limits.MaxLineBytes = defaults.MaxLineBytes
	}
	if limits.MaxRecords == 0 {
		limits.MaxRecords = defaults.MaxRecords
	}
	if limits.MaxInputBytes < 1 || limits.MaxInputBytes > 1<<40 {
		return Limits{}, errors.New("max input bytes must be between 1 and 1 TiB")
	}
	if limits.MaxCorpusBytes < 1 || limits.MaxCorpusBytes > 1<<40 {
		return Limits{}, errors.New("max corpus bytes must be between 1 and 1 TiB")
	}
	if limits.MaxLineBytes < 1 || limits.MaxLineBytes > 1<<30 {
		return Limits{}, errors.New("max line bytes must be between 1 and 1 GiB")
	}
	if limits.MaxRecords < 1 || limits.MaxRecords > 10_000_000 {
		return Limits{}, errors.New("max records must be between 1 and 10000000")
	}
	return limits, nil
}

type BuildOptions struct {
	Inputs           []string
	OutputDir        string
	Runtime          RuntimeIdentity
	Limits           Limits
	RedactSignatures bool
}

type VerifyOptions struct {
	CorpusDir string
	Runtime   RuntimeIdentity
	Limits    Limits
}

type FileDescriptor struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
	Records int    `json:"records"`
}

type Manifest struct {
	Schema         string                      `json:"schema"`
	EngineCommit   string                      `json:"engineCommit"`
	Toolchain      string                      `json:"toolchain"`
	UpstreamCommit string                      `json:"upstreamCommit"`
	Config         record.SolverConfigIdentity `json:"config"`
	ConfigSHA256   string                      `json:"configSha256"`
	Redactions     []string                    `json:"redactions"`
	Entries        FileDescriptor              `json:"entries"`
}

type Expected struct {
	Solutions []api.Solution `json:"solutions"`
	Stats     solve.Stats    `json:"stats"`
}

type Entry struct {
	Schema    string      `json:"schema"`
	ID        string      `json:"id"`
	AuctionID string      `json:"auctionId"`
	Auction   api.Auction `json:"auction"`
	Expected  Expected    `json:"expected"`
}

type Result struct {
	EntryID      string `json:"entryId"`
	OutputSHA256 string `json:"outputSha256"`
}

type Report struct {
	Schema          string   `json:"schema"`
	ManifestSHA256  string   `json:"manifestSha256"`
	EngineCommit    string   `json:"engineCommit"`
	Toolchain       string   `json:"toolchain"`
	UpstreamCommit  string   `json:"upstreamCommit"`
	ConfigSHA256    string   `json:"configSha256"`
	EntryFileSHA256 string   `json:"entryFileSha256"`
	Entries         int      `json:"entries"`
	Results         []Result `json:"results"`
	AggregateSHA256 string   `json:"aggregateSha256"`
}

func validateRuntime(runtime RuntimeIdentity) error {
	identity := record.RunIdentity{
		Schema:         record.RunIdentitySchema,
		EngineCommit:   runtime.EngineCommit,
		Toolchain:      runtime.Toolchain,
		UpstreamCommit: runtime.UpstreamCommit,
		Config:         record.SolverConfigFrom(solve.DefaultConfig()),
	}
	digest, err := identity.Config.Digest()
	if err != nil {
		return err
	}
	identity.ConfigSHA256 = digest
	if err := identity.Validate(); err != nil {
		return fmt.Errorf("runtime identity: %w", err)
	}
	return nil
}
