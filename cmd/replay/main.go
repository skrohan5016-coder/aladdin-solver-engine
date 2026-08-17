// Command replay packs recorded full auctions into an immutable corpus and
// verifies deterministic solver output without network access.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/corpus"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "replay:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("expected pack or verify subcommand")
	}
	switch arguments[0] {
	case "pack":
		return runPack(arguments[1:])
	case "verify":
		return runVerify(arguments[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", arguments[0])
	}
}

func runPack(arguments []string) error {
	flags := flag.NewFlagSet("pack", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	records := flags.String("records", "", "comma-separated record files or glob patterns")
	output := flags.String("out", "", "new corpus directory")
	commit := flags.String("source-commit", "", "optional assertion; must match the exact embedded source commit")
	redaction := flags.String("redaction", string(corpus.RedactSignatures), "none or signatures")
	maxTotalBytes := flags.Int64("max-total-bytes", 0, "maximum total recorder input bytes; 0 uses the governed default")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	paths, err := expandRecordPaths(*records)
	if err != nil {
		return err
	}
	manifest, err := corpus.Pack(context.Background(), paths, *output, corpus.PackOptions{
		SourceCommit:    *commit,
		RedactionPolicy: corpus.RedactionPolicy(*redaction),
		MaxTotalBytes:   *maxTotalBytes,
	})
	if err != nil {
		return err
	}
	fmt.Printf("packed %d case(s) at %s\n", len(manifest.Entries), *output)
	return nil
}

func runVerify(arguments []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dir := flags.String("dir", "", "sealed corpus directory")
	commit := flags.String("source-commit", "", "optional assertion; must match the exact embedded source commit")
	maxTotalBytes := flags.Int64("max-total-bytes", 0, "maximum aggregate sealed entry bytes; 0 uses the governed default")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	report, err := corpus.Replay(context.Background(), *dir, corpus.ReplayOptions{
		SourceCommit:  *commit,
		MaxTotalBytes: *maxTotalBytes,
	})
	if err != nil {
		return err
	}
	encoded, err := corpus.ReportJSON(report)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(encoded)
	return err
}

func expandRecordPaths(specification string) ([]string, error) {
	if strings.TrimSpace(specification) == "" {
		return nil, errors.New("records specification is empty")
	}
	unique := map[string]struct{}{}
	for _, item := range strings.Split(specification, ",") {
		pattern := strings.TrimSpace(item)
		if pattern == "" {
			return nil, errors.New("records specification contains an empty item")
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid records glob %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("records pattern %q matched no files", pattern)
		}
		for _, match := range matches {
			unique[match] = struct{}{}
		}
	}
	paths := make([]string, 0, len(unique))
	for path := range unique {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}
