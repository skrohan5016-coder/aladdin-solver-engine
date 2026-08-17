package solve

import (
	"bytes"
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
	// they generate.
	RequireProfitable bool
	// MaxSolutions caps how many solutions are returned per auction.
	MaxSolutions int
	// MaxOrders caps how many eligible orders are considered.
	MaxOrders int
	// MaxPools caps how many supplied liquidity entries are parsed.
	MaxPools int
}

func DefaultConfig() Config {
	return Config{
		SettlementOverheadGas: 106_000,
		PerTradeGas:           60_000,
		RequireProfitable:     true,
		MaxSolutions:          40,
		MaxOrders:             250,
		MaxPools:              2_048,
	}
}

// Stats reports what the model did with an auction. This is the raw material
// for measuring coverage and diagnosing why it was lost.
type Stats struct {
	Orders                  int            `json:"orders"`
	PoolsUsable             int            `json:"poolsUsable"`
	PoolsSkipped            map[string]int `json:"poolsSkipped,omitempty"`
	CoWMatches              int            `json:"cowMatches"`
	BaselineRoutes          int            `json:"baselineRoutes"`
	DroppedUnsupportedOrder int            `json:"droppedUnsupportedOrder"`
	DroppedNoRoute          int            `json:"droppedNoRoute"`
	DroppedLimit            int            `json:"droppedLimitPrice"`
	DroppedNotProfitable    int            `json:"droppedNotProfitable"`
	CandidateSolutions      int            `json:"candidateSolutions"`
	Solutions               int            `json:"solutions"`
}

type Result struct {
	Solutions []api.Solution
	Stats     Stats
}

// Solve produces solutions for an auction. It never signs, submits, or touches
// funds; it returns proposed settlements only.
func Solve(ctx context.Context, auction *api.Auction, cfg Config) Result {
	defaults := DefaultConfig()
	if cfg.MaxOrders <= 0 {
		cfg.MaxOrders = defaults.MaxOrders
	}
	if cfg.MaxPools <= 0 {
		cfg.MaxPools = defaults.MaxPools
	}

	result := Result{}
	pools, skipped := BuildPoolsContext(ctx, auction.Liquidity, cfg.MaxPools)
	result.Stats.PoolsUsable = len(pools)
	result.Stats.PoolsSkipped = skipped
	if ctx.Err() != nil {
		return result
	}
	graph := NewGraph(pools)
	if ctx.Err() != nil {
		return result
	}

	orders, unsupported := eligible(auction.Orders, cfg.MaxOrders)
	result.Stats.Orders = len(orders)
	result.Stats.DroppedUnsupportedOrder = unsupported

	gasPrice, ok := new(big.Int).SetString(auction.EffectiveGasPrice, 10)
	if !ok || gasPrice.Sign() < 0 {
		gasPrice = new(big.Int)
	}

	var solutions []api.Solution
	var id uint64

	// --- Pass 1: pure CoW matches. No external liquidity, minimal gas. ---
	matched := map[string]bool{}
	for _, match := range findCoWMatchesContext(ctx, orders) {
		if ctx.Err() != nil {
			break
		}
		gas, ok := sumGas(cfg.SettlementOverheadGas, cfg.PerTradeGas, cfg.PerTradeGas)
		if !ok {
			result.Stats.DroppedNotProfitable += 2
			continue
		}
		if cfg.RequireProfitable && !cowProfitable(match, gas, gasPrice, auction.Tokens) {
			// Stats count orders, not candidate solutions.
			result.Stats.DroppedNotProfitable += 2
			continue
		}
		solutions = append(solutions, match.solution(id, gas))
		id++
		matched[match.A.UID] = true
		matched[match.B.UID] = true
		result.Stats.CoWMatches++
	}

	// --- Pass 2: baseline routing, one candidate per remaining order. ---
routeLoop:
	for i := range orders {
		if ctx.Err() != nil {
			break
		}
		order := &orders[i]
		if matched[order.UID] {
			continue
		}
		solution, surplus, surplusToken, gas, why := routeOrder(ctx, order, graph, cfg)
		switch why {
		case reasonCancelled:
			break routeLoop
		case reasonNoRoute:
			result.Stats.DroppedNoRoute++
			continue
		case reasonLimit:
			result.Stats.DroppedLimit++
			continue
		}
		if cfg.RequireProfitable && !profitable(surplus, surplusToken, gas, gasPrice, auction.Tokens) {
			result.Stats.DroppedNotProfitable++
			continue
		}
		solution.ID = id
		id++
		solutions = append(solutions, solution)
		result.Stats.BaselineRoutes++
	}

	result.Stats.CandidateSolutions = len(solutions)
	if cfg.MaxSolutions <= 0 {
		solutions = nil
	} else if len(solutions) > cfg.MaxSolutions {
		solutions = solutions[:cfg.MaxSolutions]
	}
	result.Solutions = solutions
	result.Stats.Solutions = len(solutions)
	return result
}

// eligible filters out orders this model cannot honestly settle. The driver
// may add new optional fields over time; unsupported execution semantics are
// skipped rather than silently ignored.
func eligible(orders []api.Order, max int) ([]api.Order, int) {
	out := make([]api.Order, 0, len(orders))
	unsupported := 0
	for _, order := range orders {
		if len(order.PreInteractions) > 0 || len(order.PostInteractions) > 0 ||
			len(order.Wrappers) > 0 || len(order.FeePolicies) > 0 || hasNonNullJSON(order.FlashloanHint) {
			unsupported++
			continue
		}
		if order.SellTokenSource != "" && order.SellTokenSource != "erc20" {
			unsupported++
			continue
		}
		if order.BuyTokenDest != "" && order.BuyTokenDest != "erc20" {
			unsupported++
			continue
		}
		if order.Kind != "sell" && order.Kind != "buy" {
			unsupported++
			continue
		}
		if strings.EqualFold(order.SellToken, order.BuyToken) {
			unsupported++
			continue
		}
		out = append(out, order)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out, unsupported
}

func hasNonNullJSON(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

type dropReason int

const (
	reasonOK dropReason = iota
	reasonNoRoute
	reasonLimit
	reasonCancelled
)

// routeOrder builds a single-order solution through AMM liquidity.
func routeOrder(ctx context.Context, order *api.Order, graph *Graph, cfg Config) (api.Solution, *big.Int, string, uint64, dropReason) {
	sellAmount, ok1 := new(big.Int).SetString(order.SellAmount, 10)
	buyAmount, ok2 := new(big.Int).SetString(order.BuyAmount, 10)
	if !ok1 || !ok2 || sellAmount.Sign() <= 0 || buyAmount.Sign() <= 0 {
		return api.Solution{}, nil, "", 0, reasonNoRoute
	}
	sell, buy := strings.ToLower(order.SellToken), strings.ToLower(order.BuyToken)

	if order.Kind == "buy" {
		input, route, err := minInputFor(ctx, graph, sell, buy, buyAmount, sellAmount)
		if err != nil {
			return api.Solution{}, nil, "", 0, reasonCancelled
		}
		if route == nil {
			return api.Solution{}, nil, "", 0, reasonNoRoute
		}
		gas, ok := sumGas(cfg.SettlementOverheadGas, cfg.PerTradeGas, route.Gas)
		if !ok {
			return api.Solution{}, nil, "", 0, reasonNoRoute
		}
		solution := api.Solution{
			Prices:       map[string]string{sell: buyAmount.String(), buy: input.String()},
			Trades:       []api.Trade{{Kind: "fulfillment", Order: order.UID, ExecutedAmount: buyAmount.String()}},
			Interactions: hopsToInteractions(route),
			Gas:          gas,
		}
		surplus := new(big.Int).Sub(sellAmount, input)
		return solution, surplus, sell, gas, reasonOK
	}

	route, err := graph.BestRouteContext(ctx, sell, buy, sellAmount)
	if err != nil {
		return api.Solution{}, nil, "", 0, reasonCancelled
	}
	if route == nil {
		return api.Solution{}, nil, "", 0, reasonNoRoute
	}
	if route.Out.Cmp(buyAmount) < 0 {
		return api.Solution{}, nil, "", 0, reasonLimit
	}
	gas, ok := sumGas(cfg.SettlementOverheadGas, cfg.PerTradeGas, route.Gas)
	if !ok {
		return api.Solution{}, nil, "", 0, reasonNoRoute
	}
	solution := api.Solution{
		Prices:       map[string]string{sell: route.Out.String(), buy: sellAmount.String()},
		Trades:       []api.Trade{{Kind: "fulfillment", Order: order.UID, ExecutedAmount: sellAmount.String()}},
		Interactions: hopsToInteractions(route),
		Gas:          gas,
	}
	surplus := new(big.Int).Sub(route.Out, buyAmount)
	return solution, surplus, buy, gas, reasonOK
}

// minInputFor binary-searches the smallest input that still buys want.
// Route output is monotonic in input for every pool kind supported here.
func minInputFor(ctx context.Context, graph *Graph, sell, buy string, want, maxIn *big.Int) (*big.Int, *Route, error) {
	full, err := graph.BestRouteContext(ctx, sell, buy, maxIn)
	if err != nil {
		return nil, nil, err
	}
	if full == nil || full.Out.Cmp(want) < 0 {
		return nil, nil, nil
	}
	lo, hi := big.NewInt(1), new(big.Int).Set(maxIn)
	bestIn, bestRoute := new(big.Int).Set(maxIn), full
	for i := 0; i < 256 && lo.Cmp(hi) <= 0; i++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		mid := new(big.Int).Add(lo, hi)
		mid.Rsh(mid, 1)
		route, err := graph.BestRouteContext(ctx, sell, buy, mid)
		if err != nil {
			return nil, nil, err
		}
		if route != nil && route.Out.Cmp(want) >= 0 {
			bestIn, bestRoute = new(big.Int).Set(mid), route
			hi = new(big.Int).Sub(mid, big.NewInt(1))
		} else {
			lo = new(big.Int).Add(mid, big.NewInt(1))
		}
	}
	return bestIn, bestRoute, nil
}

func hopsToInteractions(route *Route) []api.Interaction {
	out := make([]api.Interaction, 0, len(route.Hops))
	for _, hop := range route.Hops {
		out = append(out, api.Interaction{
			Kind:         "liquidity",
			ID:           hop.Pool.ID,
			InputToken:   hop.TokenIn,
			OutputToken:  hop.TokenOut,
			InputAmount:  hop.AmountIn.String(),
			OutputAmount: hop.Out.String(),
		})
	}
	return out
}

// profitable compares surplus against gas cost, both in native-token wei.
func profitable(surplus *big.Int, token string, gas uint64, gasPrice *big.Int, tokens map[string]api.TokenInfo) bool {
	if surplus == nil || surplus.Sign() <= 0 {
		return false
	}
	native := toNative(surplus, token, tokens)
	if native == nil {
		return true // missing reference price: preserve coverage and let the driver score it
	}
	cost := new(big.Int).Mul(new(big.Int).SetUint64(gas), gasPrice)
	return native.Cmp(cost) > 0
}

func cowProfitable(match Match, gas uint64, gasPrice *big.Int, tokens map[string]api.TokenInfo) bool {
	surplusA := new(big.Int).Sub(match.SellB, amountOrZero(match.A.BuyAmount))
	surplusB := new(big.Int).Sub(match.SellA, amountOrZero(match.B.BuyAmount))
	if surplusA.Sign() < 0 || surplusB.Sign() < 0 {
		return false
	}
	valueA := toNative(surplusA, match.A.BuyToken, tokens)
	valueB := toNative(surplusB, match.B.BuyToken, tokens)
	if valueA == nil || valueB == nil {
		return surplusA.Sign() > 0 || surplusB.Sign() > 0
	}
	value := new(big.Int).Add(valueA, valueB)
	cost := new(big.Int).Mul(new(big.Int).SetUint64(gas), gasPrice)
	return value.Cmp(cost) > 0
}

func amountOrZero(value string) *big.Int {
	amount, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return new(big.Int)
	}
	return amount
}

// toNative converts a token amount to native wei using the auction's
// referencePrice, which is quoted per 10**18 units of the token.
func toNative(amount *big.Int, token string, tokens map[string]api.TokenInfo) *big.Int {
	for address, info := range tokens {
		if !strings.EqualFold(address, token) || info.ReferencePrice == "" {
			continue
		}
		price, ok := new(big.Int).SetString(info.ReferencePrice, 10)
		if !ok || price.Sign() < 0 {
			return nil
		}
		value := new(big.Int).Mul(amount, price)
		return value.Quo(value, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	}
	return nil
}

// ---------- CoW matching ----------

// Match is a two-sided coincidence of wants settled with no external liquidity.
type Match struct {
	A, B         *api.Order
	SellA, SellB *big.Int
}

// findCoWMatches is the context-free helper retained for focused tests.
func findCoWMatches(orders []api.Order) []Match {
	return findCoWMatchesContext(context.Background(), orders)
}

// findCoWMatchesContext pairs opposite sell orders whose limits cross.
func findCoWMatchesContext(ctx context.Context, orders []api.Order) []Match {
	type key struct{ sell, buy string }
	bucket := map[key][]*api.Order{}
	for i := range orders {
		if ctx.Err() != nil {
			return nil
		}
		order := &orders[i]
		if order.Kind != "sell" {
			continue
		}
		pair := key{strings.ToLower(order.SellToken), strings.ToLower(order.BuyToken)}
		bucket[pair] = append(bucket[pair], order)
	}

	var out []Match
	used := map[string]bool{}
	keys := make([]key, 0, len(bucket))
	for pair := range bucket {
		keys = append(keys, pair)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].sell != keys[j].sell {
			return keys[i].sell < keys[j].sell
		}
		return keys[i].buy < keys[j].buy
	})

	for _, pair := range keys {
		if ctx.Err() != nil {
			return out
		}
		reverse := key{pair.buy, pair.sell}
		if pair.sell > pair.buy {
			continue
		}
		for _, a := range bucket[pair] {
			if ctx.Err() != nil {
				return out
			}
			if used[a.UID] {
				continue
			}
			sellA, ok1 := new(big.Int).SetString(a.SellAmount, 10)
			buyA, ok2 := new(big.Int).SetString(a.BuyAmount, 10)
			if !ok1 || !ok2 || sellA.Sign() <= 0 || buyA.Sign() <= 0 {
				continue
			}
			for _, b := range bucket[reverse] {
				if ctx.Err() != nil {
					return out
				}
				if used[b.UID] {
					continue
				}
				sellB, ok1 := new(big.Int).SetString(b.SellAmount, 10)
				buyB, ok2 := new(big.Int).SetString(b.BuyAmount, 10)
				if !ok1 || !ok2 || sellB.Sign() <= 0 || buyB.Sign() <= 0 {
					continue
				}
				if sellB.Cmp(buyA) >= 0 && sellA.Cmp(buyB) >= 0 {
					out = append(out, Match{A: a, B: b, SellA: sellA, SellB: sellB})
					used[a.UID], used[b.UID] = true, true
					break
				}
			}
		}
	}
	return out
}

func (match Match) solution(id, gas uint64) api.Solution {
	x := strings.ToLower(match.A.SellToken)
	y := strings.ToLower(match.A.BuyToken)
	return api.Solution{
		ID: id,
		Prices: map[string]string{
			x: match.SellB.String(),
			y: match.SellA.String(),
		},
		Trades: []api.Trade{
			{Kind: "fulfillment", Order: match.A.UID, ExecutedAmount: match.SellA.String()},
			{Kind: "fulfillment", Order: match.B.UID, ExecutedAmount: match.SellB.String()},
		},
		Interactions: []api.Interaction{},
		Gas:          gas,
	}
}
