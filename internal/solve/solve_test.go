package solve

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
)

func bi(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("bad int " + s)
	}
	return v
}

const (
	tokA = "0x000000000000000000000000000000000000000a"
	tokB = "0x000000000000000000000000000000000000000b"
	tokC = "0x000000000000000000000000000000000000000c"
)

func sellOrder(uid, sell, buy, sellAmt, buyAmt string) api.Order {
	return api.Order{
		UID: uid, SellToken: sell, BuyToken: buy,
		SellAmount: sellAmt, BuyAmount: buyAmt, FullBuyAmount: buyAmt,
		Kind: "sell", Class: "market", Owner: "0x01",
		SellTokenSource: "erc20", BuyTokenDest: "erc20",
	}
}

func cpPool(id, a, b, ra, rb string) api.Liquidity {
	return api.Liquidity{
		Kind: "constantProduct", ID: id, Address: "0xpool" + id, GasEstimate: "90000",
		Fee:    "0.003",
		Tokens: []byte(`{"` + a + `":{"balance":"` + ra + `"},"` + b + `":{"balance":"` + rb + `"}}`),
		Router: "0xrouter",
	}
}

// assertSettlementBalances checks the core CoW invariant: for every trade,
// executedSell * price[sellToken] == executedBuy * price[buyToken], and every
// traded token carries a price. A solution failing this is rejected by the
// driver with invalidClearingPrices.
func assertSettlementBalances(t *testing.T, sol api.Solution, orders map[string]api.Order) {
	t.Helper()
	for _, tr := range sol.Trades {
		o, ok := orders[tr.Order]
		if !ok {
			t.Fatalf("solution references unknown order %s", tr.Order)
		}
		ps, oks := sol.Prices[strings.ToLower(o.SellToken)]
		pb, okb := sol.Prices[strings.ToLower(o.BuyToken)]
		if !oks || !okb {
			t.Fatalf("missing clearing price for order %s", tr.Order)
		}
		exec := bi(tr.ExecutedAmount)
		var sellAmt, buyAmt *big.Int
		if o.Kind == "sell" {
			sellAmt = exec
			buyAmt = new(big.Int).Quo(new(big.Int).Mul(exec, bi(ps)), bi(pb))
		} else {
			buyAmt = exec
			sellAmt = new(big.Int).Quo(new(big.Int).Mul(exec, bi(pb)), bi(ps))
		}
		lhs := new(big.Int).Mul(sellAmt, bi(ps))
		rhs := new(big.Int).Mul(buyAmt, bi(pb))
		if lhs.Cmp(rhs) != 0 {
			t.Errorf("order %s: settlement does not balance: %s != %s", tr.Order, lhs, rhs)
		}
		// The user must receive at least their limit price.
		if o.Kind == "sell" && buyAmt.Cmp(bi(o.BuyAmount)) < 0 {
			t.Errorf("order %s: limit price violated, got %s want >= %s", tr.Order, buyAmt, o.BuyAmount)
		}
	}
}

func TestCoWMatchProducesBalancedSettlement(t *testing.T) {
	// A sells 100 A for at least 90 B. B sells 100 B for at least 90 A.
	// The limits cross, so both can be filled outright with no AMM.
	a := sellOrder("0xaa", tokA, tokB, "100000000000000000000", "90000000000000000000")
	b := sellOrder("0xbb", tokB, tokA, "100000000000000000000", "90000000000000000000")

	auction := &api.Auction{
		Orders:            []api.Order{a, b},
		EffectiveGasPrice: "1000000000",
		Tokens:            map[string]api.TokenInfo{},
	}
	res := Solve(context.Background(), auction, DefaultConfig())

	if res.Stats.CoWMatches != 1 {
		t.Fatalf("expected 1 CoW match, got %d (solutions=%d)", res.Stats.CoWMatches, res.Stats.Solutions)
	}
	sol := res.Solutions[0]
	if len(sol.Interactions) != 0 {
		t.Errorf("a pure CoW must touch no external liquidity, got %d interactions", len(sol.Interactions))
	}
	assertSettlementBalances(t, sol, map[string]api.Order{"0xaa": a, "0xbb": b})
}

func TestCoWMatchRejectedWhenLimitsDoNotCross(t *testing.T) {
	// A wants at least 110 B for 100 A; B only offers 100 B and wants 110 A.
	a := sellOrder("0xaa", tokA, tokB, "100", "110")
	b := sellOrder("0xbb", tokB, tokA, "100", "110")
	auction := &api.Auction{
		Orders: []api.Order{a, b}, EffectiveGasPrice: "1000000000",
		Tokens: map[string]api.TokenInfo{},
	}
	res := Solve(context.Background(), auction, DefaultConfig())
	if res.Stats.CoWMatches != 0 {
		t.Errorf("non-crossing limits must not match, got %d", res.Stats.CoWMatches)
	}
}

func TestBaselineRoutingDirect(t *testing.T) {
	o := sellOrder("0x01", tokA, tokB, "1000000000000000000", "1")
	auction := &api.Auction{
		Orders: []api.Order{o},
		Liquidity: []api.Liquidity{
			cpPool("0", tokA, tokB, "1000000000000000000000", "2000000000000000000000"),
		},
		EffectiveGasPrice: "1000000000",
		Tokens:            map[string]api.TokenInfo{},
	}
	cfg := DefaultConfig()
	cfg.RequireProfitable = false
	res := Solve(context.Background(), auction, cfg)

	if res.Stats.BaselineRoutes != 1 {
		t.Fatalf("expected 1 route, got %d (noRoute=%d limit=%d)",
			res.Stats.BaselineRoutes, res.Stats.DroppedNoRoute, res.Stats.DroppedLimit)
	}
	sol := res.Solutions[0]
	if len(sol.Interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(sol.Interactions))
	}
	assertSettlementBalances(t, sol, map[string]api.Order{"0x01": o})
}

func TestBaselineRoutingTwoHop(t *testing.T) {
	// No direct A/B pool exists; the router must pivot through C.
	o := sellOrder("0x01", tokA, tokB, "1000000000000000000", "1")
	auction := &api.Auction{
		Orders: []api.Order{o},
		Liquidity: []api.Liquidity{
			cpPool("0", tokA, tokC, "1000000000000000000000", "1000000000000000000000"),
			cpPool("1", tokC, tokB, "1000000000000000000000", "1000000000000000000000"),
		},
		EffectiveGasPrice: "1000000000",
		Tokens:            map[string]api.TokenInfo{},
	}
	cfg := DefaultConfig()
	cfg.RequireProfitable = false
	res := Solve(context.Background(), auction, cfg)

	if res.Stats.BaselineRoutes != 1 {
		t.Fatalf("expected a two-hop route, got %d", res.Stats.BaselineRoutes)
	}
	if got := len(res.Solutions[0].Interactions); got != 2 {
		t.Fatalf("expected 2 interactions, got %d", got)
	}
	assertSettlementBalances(t, res.Solutions[0], map[string]api.Order{"0x01": o})
}

func TestRouterPrefersBetterPool(t *testing.T) {
	o := sellOrder("0x01", tokA, tokB, "1000000000000000000", "1")
	auction := &api.Auction{
		Orders: []api.Order{o},
		Liquidity: []api.Liquidity{
			cpPool("shallow", tokA, tokB, "10000000000000000000", "10000000000000000000"),
			cpPool("deep", tokA, tokB, "10000000000000000000000", "10000000000000000000000"),
		},
		EffectiveGasPrice: "1000000000",
		Tokens:            map[string]api.TokenInfo{},
	}
	cfg := DefaultConfig()
	cfg.RequireProfitable = false
	res := Solve(context.Background(), auction, cfg)
	if len(res.Solutions) == 0 {
		t.Fatal("no solution")
	}
	if id := res.Solutions[0].Interactions[0].ID; id != "deep" {
		t.Errorf("router picked %q, expected the deeper pool", id)
	}
}

func TestLimitPriceRespected(t *testing.T) {
	// Demands far more than the pool can deliver.
	o := sellOrder("0x01", tokA, tokB, "1000000000000000000", "9999000000000000000000")
	auction := &api.Auction{
		Orders:            []api.Order{o},
		Liquidity:         []api.Liquidity{cpPool("0", tokA, tokB, "1000000000000000000000", "1000000000000000000000")},
		EffectiveGasPrice: "1000000000",
		Tokens:            map[string]api.TokenInfo{},
	}
	res := Solve(context.Background(), api2(auction), DefaultConfig())
	if len(res.Solutions) != 0 {
		t.Errorf("must not propose a solution that violates the limit price")
	}
	if res.Stats.DroppedLimit != 1 {
		t.Errorf("expected the order to be dropped on limit price, stats=%+v", res.Stats)
	}
}

func TestBuyOrderUsesMinimalInput(t *testing.T) {
	o := api.Order{
		UID: "0x01", SellToken: tokA, BuyToken: tokB,
		SellAmount: "1000000000000000000", BuyAmount: "100000000000000000",
		FullBuyAmount: "100000000000000000",
		Kind:          "buy", Class: "market", Owner: "0x01",
		SellTokenSource: "erc20", BuyTokenDest: "erc20",
	}
	auction := &api.Auction{
		Orders:            []api.Order{o},
		Liquidity:         []api.Liquidity{cpPool("0", tokA, tokB, "1000000000000000000000", "1000000000000000000000")},
		EffectiveGasPrice: "1000000000",
		Tokens:            map[string]api.TokenInfo{},
	}
	cfg := DefaultConfig()
	cfg.RequireProfitable = false
	res := Solve(context.Background(), auction, cfg)
	if len(res.Solutions) != 1 {
		t.Fatalf("expected 1 solution, stats=%+v", res.Stats)
	}
	sol := res.Solutions[0]
	// price[sellToken] == buyAmount and price[buyToken] == actual input.
	paid := bi(sol.Prices[strings.ToLower(tokB)])
	if paid.Cmp(bi(o.SellAmount)) >= 0 {
		t.Errorf("buy order should spend less than the full sell amount, spent %s", paid)
	}
	assertSettlementBalances(t, sol, map[string]api.Order{"0x01": o})
}

func TestUnprofitableSolutionDropped(t *testing.T) {
	// Surplus is one wei of B, gas price is enormous.
	o := sellOrder("0x01", tokA, tokB, "1000000000000000000", "1")
	auction := &api.Auction{
		Orders:            []api.Order{o},
		Liquidity:         []api.Liquidity{cpPool("0", tokA, tokB, "1000000000000000000000", "1000000000000000000000")},
		EffectiveGasPrice: "100000000000000",
		Tokens: map[string]api.TokenInfo{
			tokB: {ReferencePrice: "1", AvailableBalance: "0", Trusted: false},
		},
	}
	res := Solve(context.Background(), auction, DefaultConfig())
	if len(res.Solutions) != 0 {
		t.Errorf("expected the loss-making solution to be dropped, got %d", len(res.Solutions))
	}
	if res.Stats.DroppedNotProfitable != 1 {
		t.Errorf("stats should record the drop reason, got %+v", res.Stats)
	}
}

func TestOrdersWithHooksSkipped(t *testing.T) {
	o := sellOrder("0x01", tokA, tokB, "1000000000000000000", "1")
	o.PreInteractions = []json.RawMessage{json.RawMessage(`{}`)}
	auction := &api.Auction{
		Orders:            []api.Order{o},
		Liquidity:         []api.Liquidity{cpPool("0", tokA, tokB, "1000000000000000000000", "1000000000000000000000")},
		EffectiveGasPrice: "1000000000",
		Tokens:            map[string]api.TokenInfo{},
	}
	res := Solve(context.Background(), auction, DefaultConfig())
	if res.Stats.Orders != 0 {
		t.Errorf("orders with hooks are not modelled and must be skipped")
	}
}

func TestUnsupportedPoolKindsCounted(t *testing.T) {
	auction := &api.Auction{
		Orders: []api.Order{},
		Liquidity: []api.Liquidity{
			{Kind: "stable", ID: "s0", Fee: "0.0004", Tokens: []byte(`{}`), GasEstimate: "100000"},
			{Kind: "weightedProduct", ID: "w0", Fee: "0.001", Tokens: []byte(`{}`), GasEstimate: "100000"},
		},
		EffectiveGasPrice: "1000000000",
		Tokens:            map[string]api.TokenInfo{},
	}
	res := Solve(context.Background(), auction, DefaultConfig())
	if res.Stats.PoolsSkipped["stable"] != 1 || res.Stats.PoolsSkipped["weightedProduct"] != 1 {
		t.Errorf("skipped pool kinds must be counted, got %+v", res.Stats.PoolsSkipped)
	}
}

func TestCancelledContextStopsWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	orders := make([]api.Order, 0, 50)
	for i := 0; i < 50; i++ {
		orders = append(orders, sellOrder(string(rune('a'+i%26))+"x", tokA, tokB, "1000000000000000000", "1"))
	}
	auction := &api.Auction{
		Orders:            orders,
		Liquidity:         []api.Liquidity{cpPool("0", tokA, tokB, "1000000000000000000000", "1000000000000000000000")},
		EffectiveGasPrice: "1000000000",
		Tokens:            map[string]api.TokenInfo{},
	}
	res := Solve(ctx, auction, DefaultConfig())
	if len(res.Solutions) != 0 {
		t.Errorf("a cancelled context should produce no solutions, got %d", len(res.Solutions))
	}
}

// api2 is an identity helper kept so the limit-price test reads clearly.
func api2(a *api.Auction) *api.Auction { return a }
