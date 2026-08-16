// Command contractcheck validates the pinned CoW wire fixtures and deterministic replay.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/contract"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

type manifestFixture struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	Schema   string `json:"schema"`
	Upstream struct {
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
	} `json:"upstream"`
	Fixtures []manifestFixture `json:"fixtures"`
	ReplayPairs []struct {
		Auction  string `json:"auction"`
		Solution string `json:"solution"`
	} `json:"replayPairs"`
	NotificationRoundTrips []string `json:"notificationRoundTrips"`
}

func main() {
	dir := flag.String("dir", "testdata/contracts", "contract fixture directory")
	flag.Parse()
	if err := run(*dir); err != nil {
		fmt.Fprintln(os.Stderr, "contractcheck:", err)
		os.Exit(1)
	}
	fmt.Println("contract fixtures and deterministic replay passed")
}

func run(dir string) error {
	manifestPath, err := fixturePath(dir, "manifest.json")
	if err != nil {
		return err
	}
	manifestData, err := readRegularFile(manifestPath)
	if err != nil {
		return err
	}
	if err := contract.ValidateUniqueJSON(manifestData); err != nil {
		return fmt.Errorf("validate manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(manifestData, &m); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	if m.Schema != "aladdin-contract-fixture-manifest/v1" ||
		m.Upstream.Repository != contract.UpstreamRepository || m.Upstream.Commit != contract.UpstreamCommit {
		return fmt.Errorf("fixture manifest is not bound to the accepted upstream pin")
	}
	if len(m.Fixtures) == 0 {
		return fmt.Errorf("fixture manifest is empty")
	}

	listed := make(map[string]string, len(m.Fixtures))
	for _, fixture := range m.Fixtures {
		if _, duplicate := listed[fixture.Path]; duplicate {
			return fmt.Errorf("duplicate fixture path %q", fixture.Path)
		}
		path, err := fixturePath(dir, fixture.Path)
		if err != nil {
			return err
		}
		data, err := readRegularFile(path)
		if err != nil {
			return err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(data))
		if digest != fixture.SHA256 {
			return fmt.Errorf("fixture digest mismatch: %s", fixture.Path)
		}
		switch fixture.Kind {
		case "auction":
			err = contract.ValidateAuctionJSON(data)
		case "solution":
			err = contract.ValidateSolveResponseJSON(data)
		case "notification":
			err = contract.ValidateNotificationJSON(data)
		default:
			return fmt.Errorf("unknown fixture kind %q", fixture.Kind)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", fixture.Path, err)
		}
		listed[fixture.Path] = fixture.Kind
	}
	if err := verifyFixtureInventory(dir, listed); err != nil {
		return err
	}

	for _, pair := range m.ReplayPairs {
		if listed[pair.Auction] != "auction" || listed[pair.Solution] != "solution" {
			return fmt.Errorf("replay pair is not bound to listed auction and solution fixtures: %q -> %q", pair.Auction, pair.Solution)
		}
		auctionPath, err := fixturePath(dir, pair.Auction)
		if err != nil {
			return err
		}
		auctionData, err := readRegularFile(auctionPath)
		if err != nil {
			return err
		}
		var auction api.Auction
		if err := json.Unmarshal(auctionData, &auction); err != nil {
			return fmt.Errorf("decode %s: %w", pair.Auction, err)
		}
		config := solve.DefaultConfig()
		config.RequireProfitable = false
		result := solve.Solve(context.Background(), &auction, config)
		actual, err := json.Marshal(api.SolveResponse{Solutions: result.Solutions})
		if err != nil {
			return err
		}
		solutionPath, err := fixturePath(dir, pair.Solution)
		if err != nil {
			return err
		}
		expected, err := readRegularFile(solutionPath)
		if err != nil {
			return err
		}
		actual, err = contract.NormalizeJSON(actual)
		if err != nil {
			return err
		}
		expected, err = contract.NormalizeJSON(expected)
		if err != nil {
			return err
		}
		if string(actual) != string(expected) {
			return fmt.Errorf("%s does not replay to %s\nactual: %s\nexpected: %s", pair.Auction, pair.Solution, actual, expected)
		}
	}

	for _, name := range m.NotificationRoundTrips {
		if listed[name] != "notification" {
			return fmt.Errorf("notification round trip is not bound to a listed notification fixture: %q", name)
		}
		path, err := fixturePath(dir, name)
		if err != nil {
			return err
		}
		data, err := readRegularFile(path)
		if err != nil {
			return err
		}
		var notification api.Notification
		if err := json.Unmarshal(data, &notification); err != nil {
			return err
		}
		roundTrip, err := json.Marshal(notification)
		if err != nil {
			return err
		}
		left, err := contract.NormalizeJSON(data)
		if err != nil {
			return err
		}
		right, err := contract.NormalizeJSON(roundTrip)
		if err != nil {
			return err
		}
		if string(left) != string(right) {
			return fmt.Errorf("notification metadata changed during round trip: %s", name)
		}
	}
	return nil
}

func fixturePath(dir, name string) (string, error) {
	if name == "" || filepath.IsAbs(name) || filepath.Clean(name) != name || filepath.Base(name) != name ||
		strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("unsafe fixture path %q", name)
	}
	return filepath.Join(dir, name), nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("fixture is not a regular file: %s", path)
	}
	return os.ReadFile(path)
}

func verifyFixtureInventory(dir string, listed map[string]string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var actual []string
	for _, entry := range entries {
		if entry.Name() == "manifest.json" || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return fmt.Errorf("fixture inventory contains non-regular JSON entry %q", entry.Name())
		}
		actual = append(actual, entry.Name())
	}
	sort.Strings(actual)
	var expected []string
	for name := range listed {
		expected = append(expected, name)
	}
	sort.Strings(expected)
	if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
		return fmt.Errorf("fixture inventory mismatch: listed=%v actual=%v", expected, actual)
	}
	return nil
}
