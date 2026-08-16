package solve

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/amm"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
)

func TestUnsupportedExecutionSemanticsAreSkipped(t *testing.T) {
	withFee := sellOrder("fee", tokA, tokB, "100", "90")
	factor := 0.5
	withFee.FeePolicies = []api.FeePolicy{{Kind: "surplus", Factor: &factor}}

	withWrapper := sellOrder("wrapper", tokA, tokB, "100", "90")
	withWrapper.Wrappers = []json.RawMessage{json.RawMessage(`{"target":"0x1"}`)}

	result := Solve(context.Background(), &api.Auction{
		Orders:            []api.Order{withFee, withWrapper},
		EffectiveGasPrice: "1",
	}, DefaultConfig())
	if result.Stats.Orders != 0 || result.Stats.DroppedUnsupportedOrder != 2 {
		t.Fatalf("unsupported orders were not safely skipped: %+v", result.Stats)
	}
}

func TestUnprofitableCoWMatchIsNotReturned(t *testing.T) {
	a := sellOrder("a", tokA, tokB, "100000000000000000000", "99000000000000000000")
	b := sellOrder("b", tokB, tokA, "100000000000000000000", "99000000000000000000")
	result := Solve(context.Background(), &api.Auction{
		Orders:            []api.Order{a, b},
		EffectiveGasPrice: "1000000000000000000",
		Tokens: map[string]api.TokenInfo{
			tokA: {ReferencePrice: "1000000000000000000"},
			tokB: {ReferencePrice: "1000000000000000000"},
		},
	}, DefaultConfig())
	if len(result.Solutions) != 0 || result.Stats.CoWMatches != 0 {
		t.Fatalf("loss-making CoW match was returned: %+v", result)
	}
	if result.Stats.DroppedNotProfitable != 2 {
		t.Fatalf("expected both matched orders to be counted, got %+v", result.Stats)
	}
}

func TestStablePoolParsesAndQuotesEveryPair(t *testing.T) {
	tokens := json.RawMessage(`{
		"0x000000000000000000000000000000000000000a":{"balance":"1000000000000000000000000","scalingFactor":"1"},
		"0x000000000000000000000000000000000000000b":{"balance":"1000000000000000000000000","scalingFactor":"1"},
		"0x000000000000000000000000000000000000000c":{"balance":"1000000000000000000000000","scalingFactor":"1"}
	}`)
	pools, skipped := BuildPools([]api.Liquidity{{
		Kind: "stable", ID: "1", Address: "0xpool", GasEstimate: "88892",
		Tokens: tokens, Fee: "0.0004", AmplificationParameter: "100",
	}})
	if len(pools) != 1 || len(skipped) != 0 {
		t.Fatalf("stable pool not accepted: pools=%d skipped=%v", len(pools), skipped)
	}
	if !pools[0].Supports(tokA, tokC) {
		t.Fatal("three-token stable pool does not expose A/C")
	}
	out, err := pools[0].QuoteExactInPair(tokA, tokC, big.NewInt(1_000_000_000_000_000_000))
	if err != nil || out.Sign() <= 0 {
		t.Fatalf("stable A/C quote failed: out=%v err=%v", out, err)
	}
}

func TestMalformedFeeIsSkipped(t *testing.T) {
	liquidity := cpPool("1", tokA, tokB, "1000000", "1000000")
	liquidity.Fee = "-0.01"
	pools, skipped := BuildPools([]api.Liquidity{liquidity})
	if len(pools) != 0 || skipped["constantProduct"] != 1 {
		t.Fatalf("negative fee was accepted: pools=%d skipped=%v", len(pools), skipped)
	}
}

func TestPoolResourceLimitIsReported(t *testing.T) {
	liquidity := []api.Liquidity{
		cpPool("1", tokA, tokB, "1000000", "1000000"),
		cpPool("2", tokA, tokB, "1000000", "1000000"),
		cpPool("3", tokA, tokB, "1000000", "1000000"),
	}
	pools, skipped := BuildPoolsContext(context.Background(), liquidity, 2)
	if len(pools) != 2 || skipped["resourceLimit"] != 1 {
		t.Fatalf("pool limit not enforced: pools=%d skipped=%v", len(pools), skipped)
	}
}

func TestEqualOutputRouteTieIsDeterministic(t *testing.T) {
	pool := func(id string) *amm.Pool {
		return &amm.Pool{
			ID: id, Kind: "constantProduct", TokenA: tokA, TokenB: tokB,
			ReserveA: big.NewInt(1_000_000), ReserveB: big.NewInt(1_000_000),
			FeeNum: big.NewInt(3), FeeDen: big.NewInt(1000), GasEstimate: 90_000,
		}
	}
	graph := NewGraph([]*amm.Pool{pool("2"), pool("1")})
	for i := 0; i < 100; i++ {
		route := graph.BestRoute(tokA, tokB, big.NewInt(1000))
		if route == nil || route.Hops[0].Pool.ID != "1" {
			t.Fatalf("non-deterministic tie break on iteration %d: %+v", i, route)
		}
	}
}

func TestRoutingObservesCancelledContext(t *testing.T) {
	pool := &amm.Pool{
		ID: "1", Kind: "constantProduct", TokenA: tokA, TokenB: tokB,
		ReserveA: big.NewInt(1_000_000), ReserveB: big.NewInt(1_000_000),
		FeeNum: big.NewInt(3), FeeDen: big.NewInt(1000), GasEstimate: 90_000,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	route, err := NewGraph([]*amm.Pool{pool}).BestRouteContext(ctx, tokA, tokB, big.NewInt(1000))
	if route != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("route=%v err=%v, want context cancellation", route, err)
	}
}

func TestCancellationIsNotCountedAsNoRoute(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	order := sellOrder("cancelled", tokA, tokB, "1000", "1")
	result := Solve(ctx, &api.Auction{
		Orders:            []api.Order{order},
		Liquidity:         []api.Liquidity{cpPool("1", tokA, tokB, "1000000", "1000000")},
		EffectiveGasPrice: "1",
	}, DefaultConfig())
	if result.Stats.DroppedNoRoute != 0 || result.Stats.CandidateSolutions != 0 {
		t.Fatalf("cancellation was misclassified: %+v", result.Stats)
	}
}

func TestCandidateCountIsRecordedBeforeReturnCap(t *testing.T) {
	orders := []api.Order{
		sellOrder("1", tokA, tokB, "1000", "1"),
		sellOrder("2", tokA, tokB, "1000", "1"),
		sellOrder("3", tokA, tokB, "1000", "1"),
	}
	cfg := DefaultConfig()
	cfg.RequireProfitable = false
	cfg.MaxSolutions = 1
	result := Solve(context.Background(), &api.Auction{
		Orders:            orders,
		Liquidity:         []api.Liquidity{cpPool("1", tokA, tokB, "1000000", "1000000")},
		EffectiveGasPrice: "1",
	}, cfg)
	if result.Stats.CandidateSolutions != 3 || result.Stats.Solutions != 1 || len(result.Solutions) != 1 {
		t.Fatalf("candidate/return cap mismatch: %+v", result)
	}
}
