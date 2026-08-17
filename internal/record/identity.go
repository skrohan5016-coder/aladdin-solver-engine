package record

import (
	"fmt"
	"os"
	"time"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/buildinfo"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/contract"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

type ReplayIdentity struct {
	Engine         buildinfo.Identity   `json:"engine"`
	UpstreamCommit string               `json:"upstreamCommit"`
	Config         solve.ConfigSnapshot `json:"config"`
	ConfigSHA256   string               `json:"configSha256"`
}

type Options struct {
	KeepAuctions bool
	Config       solve.Config
	EngineCommit string
}

func NewWithOptions(dir string, options Options) (*Recorder, error) {
	if dir == "" {
		return nil, fmt.Errorf("record directory is empty")
	}
	config, digest, err := solve.SnapshotConfig(options.Config)
	if err != nil {
		return nil, fmt.Errorf("identify solver config: %w", err)
	}
	engine := buildinfo.Current()
	if options.EngineCommit != "" && options.EngineCommit != engine.Commit {
		return nil, fmt.Errorf("engine commit assertion mismatch: embedded %q requested %q", engine.Commit, options.EngineCommit)
	}
	if options.KeepAuctions && !buildinfo.ValidCommit(engine.Commit) {
		return nil, fmt.Errorf("full-auction recording requires an exact embedded engine commit, got %q", engine.Commit)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create record directory: %w", err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		return nil, fmt.Errorf("secure record directory: %w", err)
	}
	return &Recorder{
		dir:          dir,
		KeepAuctions: options.KeepAuctions,
		now:          time.Now,
		identity: ReplayIdentity{
			Engine:         engine,
			UpstreamCommit: contract.UpstreamCommit,
			Config:         config,
			ConfigSHA256:   digest,
		},
	}, nil
}
