package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPercentileNearestRank(t *testing.T) {
	values := []int64{10, 20, 30, 40, 50}
	cases := []struct {
		percent int
		want    int64
	}{
		{percent: 0, want: 10},
		{percent: 50, want: 30},
		{percent: 95, want: 50},
		{percent: 100, want: 50},
	}
	for _, tc := range cases {
		if got := percentile(values, tc.percent); got != tc.want {
			t.Errorf("p%d = %d, want %d", tc.percent, got, tc.want)
		}
	}
	if got := percentile([]int64{10, 20}, 95); got != 20 {
		t.Fatalf("small-sample p95 = %d, want 20", got)
	}
	if got := percentile(nil, 95); got != 0 {
		t.Fatalf("empty percentile = %d, want 0", got)
	}
}

func TestEachLineIsSortedAndPropagatesLocation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "auctions-b.jsonl"), []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auctions-a.jsonl"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var seen []string
	if err := eachLine(dir, "auctions-*.jsonl", func(path string, line int, data []byte) error {
		seen = append(seen, filepath.Base(path)+":"+string(data))
		if line != 1 {
			t.Fatalf("line = %d, want 1", line)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "auctions-a.jsonl:a" || seen[1] != "auctions-b.jsonl:b" {
		t.Fatalf("unexpected order: %v", seen)
	}

	want := errors.New("bad evidence")
	if err := eachLine(dir, "auctions-a.jsonl", func(string, int, []byte) error { return want }); !errors.Is(err, want) {
		t.Fatalf("callback error = %v, want %v", err, want)
	}
}
