package solve

import (
	"context"
	"math"
	"math/big"
	"testing"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
)

func TestSumGasRejectsOverflow(t *testing.T) {
	if _, ok := sumGas(math.MaxUint64, 1); ok {
		t.Fatal("gas addition overflow was accepted")
	}
}

func TestTwoHopRouteRejectsGasOverflow(t *testing.T) {
	liquidity := []api.Liquidity{
		cpPool("1", tokA, tokC, "1000000000000000000000", "1000000000000000000000"),
		cpPool("2", tokC, tokB, "1000000000000000000000", "1000000000000000000000"),
	}
	liquidity[0].GasEstimate = "18446744073709551615"
	liquidity[1].GasEstimate = "1"
	pools, skipped := BuildPools(liquidity)
	if len(pools) != 2 || len(skipped) != 0 {
		t.Fatalf("unexpected pool parse result: pools=%d skipped=%v", len(pools), skipped)
	}
	route := NewGraph(pools).BestRoute(tokA, tokB, big.NewInt(1_000_000))
	if route != nil {
		t.Fatal("router returned a path whose gas estimate overflowed uint64")
	}
}

func TestSettlementGasOverflowDropsCandidate(t *testing.T) {
	order := sellOrder("0xoverflow", tokA, tokB, "1000000000000000000", "1")
	auction := &api.Auction{
		Orders:            []api.Order{order},
		Liquidity:         []api.Liquidity{cpPool("1", tokA, tokB, "1000000000000000000000", "1000000000000000000000")},
		EffectiveGasPrice: "1",
		Tokens:            map[string]api.TokenInfo{},
	}
	config := DefaultConfig()
	config.RequireProfitable = false
	config.SettlementOverheadGas = math.MaxUint64
	config.PerTradeGas = 1
	result := Solve(context.Background(), auction, config)
	if len(result.Solutions) != 0 {
		t.Fatal("solver returned a candidate with wrapped settlement gas")
	}
}
