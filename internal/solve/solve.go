package solve

import (
	"context"
	"math/big"
	"sort"
	"strings"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
)

// Config tunes the model. These are the knobs worth sweeping once shadow data
// starts coming in; they are deliberately not compiled in as constants.
type Config struct {
	// SettlementOverheadGas is the fixed gas cost of a settlement before any
	// liquidity interaction.
	SettlementOverheadGas uint64
	// PerTradeGas is the marginal gas cost of including one more trade.
	PerTradeGas uint64
	// RequireProfitable drops solutions whose gas cost exceeds the surplus
	// they generate. Settlement success rate discounts bid quality, so
	// bidding on batches you would not actually settle is costly.
	RequireProfitable bool
	// MaxSolutions caps how many solutions are returned per auction.
	MaxSolutions int
	// MaxOrders caps how many orders are considered, newest-value first.
	MaxOrders int
}

func DefaultConfig() Config {
	return Config{
		SettlementOverheadGas: 106_000,
		PerTradeGas:           60_000,
		RequireProfitable:     true,
		MaxSolutions:          40,
		MaxOrders:             250,
	}
}

// Stats reports what the model did with an auction. This is the raw material
// for answering "how often would we have beaten the winner".
type Stats struct {
	Orders               int            `json:"orders"`
	PoolsUsable          int            `json:"poolsUsable"`
	PoolsSkipped         map[string]int `json:"poolsSkipped,omitempty"`
	CoWMatches           int            `json:"cowMatches"`
	BaselineRoutes       int            `json:"baselineRoutes"`
	DroppedNoRoute       int            `json:"droppedNoRoute"`
	DroppedLimit         int            `json:"droppedLimitPrice"`
	DroppedNotProfitable int            `json:"droppedNotProfitable"`
	Solutions            int            `json:"solutions"`
}

type Result struct {
	Solutions []api.Solution
	Stats     Stats
}

// Solve produces solutions for an auction. It never signs, submits, or touches
// funds; it returns proposed settlements only.
func Solve(ctx context.Context, a *api.Auction, cfg Config) Result {
	res := Result{}
	pools, skipped := BuildPools(a.Liquidity)
	res.Stats.PoolsUsable = len(pools)
	res.Stats.PoolsSkipped = skipped
	graph := NewGraph(pools)

	orders := eligible(a.Orders, cfg.MaxOrders)
	res.Stats.Orders = len(orders)

	gasPrice, ok := new(big.Int).SetString(a.EffectiveGasPrice, 10)
	if !ok || gasPrice.Sign() < 0 {
		gasPrice = new(big.Int)
	}

	var solutions []api.Solution
	var id uint64

	// --- Pass 1: pure CoW matches. No external liquidity, minimal gas. ---
	matched := map[string]bool{}
	for _, m := range findCoWMatches(orders) {
		if err := ctx.Err(); err != nil {
			break
		}
		sol := m.solution(id, cfg)
		id++
		solutions = append(solutions, sol)
		matched[m.A.UID] = true
		matched[m.B.UID] = true
		res.Stats.CoWMatches++
	}

	// --- Pass 2: baseline routing, one solution per remaining order. ---
	for i := range orders {
		if err := ctx.Err(); err != nil {
			break
		}
		o := &orders[i]
		if matched[o.UID] {
			continue
		}
		sol, surplus, surplusToken, gas, why := routeOrder(o, graph, cfg)
		switch why {
		case reasonNoRoute:
			res.Stats.DroppedNoRoute++
			continue
		case reasonLimit:
			res.Stats.DroppedLimit++
			continue
		}
		if cfg.RequireProfitable && !profitable(surplus, surplusToken, gas, gasPrice, a.Tokens) {
			res.Stats.DroppedNotProfitable++
			continue
		}
		sol.ID = id
		id++
		solutions = append(solutions, sol)
		res.Stats.BaselineRoutes++
	}

	if len(solutions) > cfg.MaxSolutions {
		solutions = solutions[:cfg.MaxSolutions]
	}
	res.Solutions = solutions
	res.Stats.Solutions = len(solutions)
	return res
}

// eligible filters out orders this model cannot honestly settle.
func eligible(orders []api.Order, max int) []api.Order {
	out := make([]api.Order, 0, len(orders))
	for _, o := range orders {
		if len(o.PreInteractions) > 0 || len(o.PostInteractions) > 0 {
			continue // hooks are not modelled yet
		}
		if o.SellTokenSource != "" && o.SellTokenSource != "erc20" {
			continue
		}
		if o.BuyTokenDest != "" && o.BuyTokenDest != "erc20" {
			continue
		}
		if strings.EqualFold(o.SellToken, o.BuyToken) {
			continue
		}
		out = append(out, o)
	}
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

type dropReason int

const (
	reasonOK dropReason = iota
	reasonNoRoute
	reasonLimit
)

// routeOrder builds a single-order solution through AMM liquidity.
func routeOrder(o *api.Order, g *Graph, cfg Config) (api.Solution, *big.Int, string, uint64, dropReason) {
	sellAmt, ok1 := new(big.Int).SetString(o.SellAmount, 10)
	buyAmt, ok2 := new(big.Int).SetString(o.BuyAmount, 10)
	if !ok1 || !ok2 || sellAmt.Sign() <= 0 || buyAmt.Sign() <= 0 {
		return api.Solution{}, nil, "", 0, reasonNoRoute
	}
	sell, buy := strings.ToLower(o.SellToken), strings.ToLower(o.BuyToken)

	if o.Kind == "buy" {
		in, route := minInputFor(g, sell, buy, buyAmt, sellAmt)
		if route == nil {
			return api.Solution{}, nil, "", 0, reasonNoRoute
		}
		gas := cfg.SettlementOverheadGas + cfg.PerTradeGas + route.Gas
		sol := api.Solution{
			Prices:       map[string]string{sell: buyAmt.String(), buy: in.String()},
			Trades:       []api.Trade{{Kind: "fulfillment", Order: o.UID, ExecutedAmount: buyAmt.String()}},
			Interactions: hopsToInteractions(route),
			Gas:          gas,
		}
		surplus := new(big.Int).Sub(sellAmt, in) // saved sell token
		return sol, surplus, sell, gas, reasonOK
	}

	// sell order
	route := g.BestRoute(sell, buy, sellAmt)
	if route == nil {
		return api.Solution{}, nil, "", 0, reasonNoRoute
	}
	if route.Out.Cmp(buyAmt) < 0 {
		return api.Solution{}, nil, "", 0, reasonLimit
	}
	gas := cfg.SettlementOverheadGas + cfg.PerTradeGas + route.Gas
	sol := api.Solution{
		Prices:       map[string]string{sell: route.Out.String(), buy: sellAmt.String()},
		Trades:       []api.Trade{{Kind: "fulfillment", Order: o.UID, ExecutedAmount: sellAmt.String()}},
		Interactions: hopsToInteractions(route),
		Gas:          gas,
	}
	surplus := new(big.Int).Sub(route.Out, buyAmt)
	return sol, surplus, buy, gas, reasonOK
}

// minInputFor binary-searches the smallest input that still buys want.
// Route output is monotonic in input for every pool kind supported here.
func minInputFor(g *Graph, sell, buy string, want, maxIn *big.Int) (*big.Int, *Route) {
	full := g.BestRoute(sell, buy, maxIn)
	if full == nil || full.Out.Cmp(want) < 0 {
		return nil, nil
	}
	lo, hi := big.NewInt(1), new(big.Int).Set(maxIn)
	bestIn, bestRoute := new(big.Int).Set(maxIn), full
	for i := 0; i < 64 && lo.Cmp(hi) < 0; i++ {
		mid := new(big.Int).Add(lo, hi)
		mid.Rsh(mid, 1)
		if mid.Sign() <= 0 {
			break
		}
		r := g.BestRoute(sell, buy, mid)
		if r != nil && r.Out.Cmp(want) >= 0 {
			bestIn, bestRoute = mid, r
			hi = new(big.Int).Sub(mid, big.NewInt(1))
		} else {
			lo = new(big.Int).Add(mid, big.NewInt(1))
		}
	}
	return bestIn, bestRoute
}

func hopsToInteractions(r *Route) []api.Interaction {
	out := make([]api.Interaction, 0, len(r.Hops))
	for _, h := range r.Hops {
		out = append(out, api.Interaction{
			Kind:         "liquidity",
			ID:           h.Pool.ID,
			InputToken:   h.TokenIn,
			OutputToken:  h.TokenOut,
			InputAmount:  h.AmountIn.String(),
			OutputAmount: h.Out.String(),
		})
	}
	return out
}

// profitable compares surplus against gas cost, both in native token wei.
func profitable(surplus *big.Int, token string, gas uint64, gasPrice *big.Int, toks map[string]api.TokenInfo) bool {
	if surplus == nil || surplus.Sign() <= 0 {
		return false
	}
	native := toNative(surplus, token, toks)
	if native == nil {
		return true // no reference price: do not punish the order for it
	}
	cost := new(big.Int).Mul(new(big.Int).SetUint64(gas), gasPrice)
	return native.Cmp(cost) > 0
}

// toNative converts a token amount to native wei using the auction's
// referencePrice, which is quoted per 10**18 units of the token.
func toNative(amount *big.Int, token string, toks map[string]api.TokenInfo) *big.Int {
	for addr, info := range toks {
		if !strings.EqualFold(addr, token) || info.ReferencePrice == "" {
			continue
		}
		p, ok := new(big.Int).SetString(info.ReferencePrice, 10)
		if !ok {
			return nil
		}
		v := new(big.Int).Mul(amount, p)
		return v.Quo(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	}
	return nil
}

// ---------- CoW matching ----------

// Match is a two-sided coincidence of wants settled with no external liquidity.
type Match struct {
	A, B         *api.Order
	SellA, SellB *big.Int
}

// findCoWMatches pairs opposite sell orders that can be swapped outright.
//
// If A sells sA of tokenX wanting at least bA of tokenY, and B sells sB of
// tokenY wanting at least bB of tokenX, then swapping their full amounts is
// feasible exactly when sB >= bA and sA >= bB. The clearing price vector
// {X: sB, Y: sA} balances the settlement by construction.
func findCoWMatches(orders []api.Order) []Match {
	type key struct{ sell, buy string }
	bucket := map[key][]*api.Order{}
	for i := range orders {
		o := &orders[i]
		if o.Kind != "sell" {
			continue
		}
		bucket[key{strings.ToLower(o.SellToken), strings.ToLower(o.BuyToken)}] = append(
			bucket[key{strings.ToLower(o.SellToken), strings.ToLower(o.BuyToken)}], o)
	}

	var out []Match
	used := map[string]bool{}
	keys := make([]key, 0, len(bucket))
	for k := range bucket {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].sell != keys[j].sell {
			return keys[i].sell < keys[j].sell
		}
		return keys[i].buy < keys[j].buy
	})

	for _, k := range keys {
		rev := key{k.buy, k.sell}
		if k.sell > k.buy {
			continue // handle each unordered pair once
		}
		for _, a := range bucket[k] {
			if used[a.UID] {
				continue
			}
			sA, ok := new(big.Int).SetString(a.SellAmount, 10)
			bA, ok2 := new(big.Int).SetString(a.BuyAmount, 10)
			if !ok || !ok2 {
				continue
			}
			for _, b := range bucket[rev] {
				if used[b.UID] {
					continue
				}
				sB, ok := new(big.Int).SetString(b.SellAmount, 10)
				bB, ok2 := new(big.Int).SetString(b.BuyAmount, 10)
				if !ok || !ok2 {
					continue
				}
				if sB.Cmp(bA) >= 0 && sA.Cmp(bB) >= 0 {
					out = append(out, Match{A: a, B: b, SellA: sA, SellB: sB})
					used[a.UID], used[b.UID] = true, true
					break
				}
			}
		}
	}
	return out
}

func (m Match) solution(id uint64, cfg Config) api.Solution {
	x := strings.ToLower(m.A.SellToken)
	y := strings.ToLower(m.A.BuyToken)
	return api.Solution{
		ID: id,
		Prices: map[string]string{
			x: m.SellB.String(),
			y: m.SellA.String(),
		},
		Trades: []api.Trade{
			{Kind: "fulfillment", Order: m.A.UID, ExecutedAmount: m.SellA.String()},
			{Kind: "fulfillment", Order: m.B.UID, ExecutedAmount: m.SellB.String()},
		},
		Interactions: []api.Interaction{},
		Gas:          cfg.SettlementOverheadGas + 2*cfg.PerTradeGas,
	}
}
