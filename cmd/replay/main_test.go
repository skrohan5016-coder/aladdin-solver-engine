package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExpandRecordPathsIsSortedAndDeduplicated(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.jsonl", "a.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := expandRecordPaths(filepath.Join(dir, "*.jsonl") + "," + filepath.Join(dir, "a.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.jsonl"), filepath.Join(dir, "b.jsonl")}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestExpandRecordPathsRejectsNoMatch(t *testing.T) {
	if _, err := expandRecordPaths(filepath.Join(t.TempDir(), "*.jsonl")); err == nil {
		t.Fatal("empty glob was accepted")
	}
}
