// Command contractcheck validates the pinned CoW wire fixtures and deterministic replay.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/contract"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

type manifest struct {
	Schema   string `json:"schema"`
	Upstream struct {
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
	} `json:"upstream"`
	Fixtures []struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	} `json:"fixtures"`
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
	manifestData, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	var m manifest
	if err := json.Unmarshal(manifestData, &m); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	if m.Schema != "aladdin-contract-fixture-manifest/v1" ||
		m.Upstream.Repository != contract.UpstreamRepository || m.Upstream.Commit != contract.UpstreamCommit {
		return fmt.Errorf("fixture manifest is not bound to the accepted upstream pin")
	}

	for _, fixture := range m.Fixtures {
		data, err := os.ReadFile(filepath.Join(dir, fixture.Path))
		if err != nil {
			return err
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
	}

	for _, pair := range m.ReplayPairs {
		auctionData, err := os.ReadFile(filepath.Join(dir, pair.Auction))
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
		expected, err := os.ReadFile(filepath.Join(dir, pair.Solution))
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
		data, err := os.ReadFile(filepath.Join(dir, name))
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
