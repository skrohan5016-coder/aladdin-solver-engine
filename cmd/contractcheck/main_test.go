package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixturePathRejectsEscapes(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../outside.json", "nested/file.json", `nested\file.json`, "/tmp/file.json"} {
		if _, err := fixturePath(t.TempDir(), name); err == nil {
			t.Errorf("unsafe path %q was accepted", name)
		}
	}
	if _, err := fixturePath(t.TempDir(), "auction.json"); err != nil {
		t.Fatalf("safe fixture path was rejected: %v", err)
	}
}

func TestRunRejectsFixtureDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	notification := []byte(`{"auctionId":"42","solutionId":7,"kind":"success"}`)
	if err := os.WriteFile(filepath.Join(dir, "notification.json"), notification, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"schema":"aladdin-contract-fixture-manifest/v1",
		"upstream":{"repository":"cowprotocol/services","commit":"20b3a62f222ad278502fb7e85cae4938e7f26f65"},
		"fixtures":[{"path":"notification.json","kind":"notification","sha256":"` + strings.Repeat("0", 64) + `"}],
		"replayPairs":[],
		"notificationRoundTrips":[]
	}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(dir); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("digest mismatch was not rejected: %v", err)
	}

	digest := fmt.Sprintf("%x", sha256.Sum256(notification))
	manifest = strings.Replace(manifest, strings.Repeat("0", 64), digest, 1)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(dir); err != nil {
		t.Fatalf("valid hashed fixture failed: %v", err)
	}
}

func TestRunRejectsUnlistedFixture(t *testing.T) {
	dir := t.TempDir()
	notification := []byte(`{"auctionId":"42","solutionId":7,"kind":"success"}`)
	digest := fmt.Sprintf("%x", sha256.Sum256(notification))
	if err := os.WriteFile(filepath.Join(dir, "notification.json"), notification, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unlisted.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"schema":"aladdin-contract-fixture-manifest/v1",
		"upstream":{"repository":"cowprotocol/services","commit":"20b3a62f222ad278502fb7e85cae4938e7f26f65"},
		"fixtures":[{"path":"notification.json","kind":"notification","sha256":"` + digest + `"}],
		"replayPairs":[],
		"notificationRoundTrips":[]
	}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(dir); err == nil || !strings.Contains(err.Error(), "inventory mismatch") {
		t.Fatalf("unlisted fixture was not rejected: %v", err)
	}
}
