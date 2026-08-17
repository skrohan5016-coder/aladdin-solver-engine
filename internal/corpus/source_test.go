package corpus

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSealsSourceDigestAndRejectsUnterminatedJSONL(t *testing.T) {
	input, identity := replayableInput(t)
	runtimeIdentity := RuntimeIdentity{
		EngineCommit:   identity.EngineCommit,
		Toolchain:      identity.Toolchain,
		UpstreamCommit: identity.UpstreamCommit,
	}
	output := filepath.Join(t.TempDir(), "corpus")
	manifest, err := Build(context.Background(), BuildOptions{
		Inputs:    []string{input},
		OutputDir: output,
		Runtime:   runtimeIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Sources) != 1 || !validSHA256(manifest.Sources[0].SHA256) || manifest.Sources[0].Records != 1 {
		t.Fatalf("source file was not sealed: %+v", manifest.Sources)
	}

	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatal("test recorder input is not newline terminated")
	}
	partial := filepath.Join(t.TempDir(), "partial.jsonl")
	if err := os.WriteFile(partial, data[:len(data)-1], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), BuildOptions{
		Inputs:    []string{partial},
		OutputDir: filepath.Join(t.TempDir(), "corpus"),
		Runtime:   runtimeIdentity,
	}); err == nil {
		t.Fatal("unterminated source JSONL was accepted")
	}
}
