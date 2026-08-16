package solve

import (
	"context"
	"math/big"
	"sort"
	"strings"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/amm"
)

// Hop is one pool traversal in a route.
type Hop struct {
	Pool     *amm.Pool
	TokenIn  string
	TokenOut string
	AmountIn *big.Int
	Out      *big.Int
}

// Route is a full path from sell token to buy token.
type Route struct {
	Hops []Hop
	Out  *big.Int
	Gas  uint64
}

// Graph indexes pools by token for path finding.
type Graph struct {
	byToken map[string][]*amm.Pool
	// intermediates are the tokens allowed as a 2-hop pivot, ordered by how
	// many pools reference them (deepest connectivity first).
	intermediates []string
}

func NewGraph(pools []*amm.Pool) *Graph {
	graph := &Graph{byToken: map[string][]*amm.Pool{}}
	count := map[string]int{}
	for _, pool := range pools {
		for _, token := range pool.AllTokens() {
			token = strings.ToLower(token)
			graph.byToken[token] = append(graph.byToken[token], pool)
			count[token]++
		}
	}
	for token, n := range count {
		if n >= 2 {
			graph.intermediates = append(graph.intermediates, token)
		}
	}
	// Map iteration is deliberately removed from the final ordering. Equal
	// connectivity is broken lexicographically so replay is deterministic.
	sort.Slice(graph.intermediates, func(i, j int) bool {
		left, right := graph.intermediates[i], graph.intermediates[j]
		if count[left] != count[right] {
			return count[left] > count[right]
		}
		return left < right
	})
	if len(graph.intermediates) > maxIntermediates {
		graph.intermediates = graph.intermediates[:maxIntermediates]
	}
	return graph
}

const (
	maxIntermediates = 12
	maxPoolsPerToken = 24
)

// BestRoute is the context-free helper used by focused math tests.
func (g *Graph) BestRoute(sellToken, buyToken string, amountIn *big.Int) *Route {
	route, _ := g.BestRouteContext(context.Background(), sellToken, buyToken, amountIn)
	return route
}

// BestRouteContext finds the highest-output path while observing the auction
// deadline between every bounded quote. Individual pool quotes are themselves
// bounded by their arithmetic iteration limits.
func (g *Graph) BestRouteContext(ctx context.Context, sellToken, buyToken string, amountIn *big.Int) (*Route, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sellToken, buyToken = strings.ToLower(sellToken), strings.ToLower(buyToken)
	if sellToken == buyToken || amountIn == nil || amountIn.Sign() <= 0 {
		return nil, nil
	}
	var best *Route

	// --- direct ---
	for _, pool := range g.poolsFor(sellToken) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !pool.Supports(sellToken, buyToken) {
			continue
		}
		out, err := pool.QuoteExactInPair(sellToken, buyToken, amountIn)
		if err != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		best = better(best, &Route{
			Hops: []Hop{{Pool: pool, TokenIn: sellToken, TokenOut: buyToken, AmountIn: amountIn, Out: out}},
			Out:  out,
			Gas:  pool.GasEstimate,
		})
	}

	// --- two hop ---
	for _, mid := range g.intermediates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if mid == sellToken || mid == buyToken {
			continue
		}
		for _, first := range g.poolsFor(sellToken) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !first.Supports(sellToken, mid) {
				continue
			}
			midAmount, err := first.QuoteExactInPair(sellToken, mid, amountIn)
			if err != nil {
				continue
			}
			for _, second := range g.poolsFor(mid) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				// Reusing one stateful pool would require applying the first swap's
				// reserve/tick transition before quoting the second.
				if second == first || !second.Supports(mid, buyToken) {
					continue
				}
				out, err := second.QuoteExactInPair(mid, buyToken, midAmount)
				if err != nil {
					continue
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				gas, ok := sumGas(first.GasEstimate, second.GasEstimate)
				if !ok {
					continue
				}
				best = better(best, &Route{
					Hops: []Hop{
						{Pool: first, TokenIn: sellToken, TokenOut: mid, AmountIn: amountIn, Out: midAmount},
						{Pool: second, TokenIn: mid, TokenOut: buyToken, AmountIn: midAmount, Out: out},
					},
					Out: out,
					Gas: gas,
				})
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return best, nil
}

func (g *Graph) poolsFor(token string) []*amm.Pool {
	pools := g.byToken[token]
	if len(pools) > maxPoolsPerToken {
		return pools[:maxPoolsPerToken]
	}
	return pools
}

func better(a, b *Route) *Route {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if cmp := b.Out.Cmp(a.Out); cmp != 0 {
		if cmp > 0 {
			return b
		}
		return a
	}
	if b.Gas != a.Gas {
		if b.Gas < a.Gas {
			return b
		}
		return a
	}
	if routeKey(b) < routeKey(a) {
		return b
	}
	return a
}

func hopKey(hop *Hop) string {
	return hop.Pool.Kind + "\x00" + hop.Pool.ID + "\x00" + hop.TokenIn + "\x00" + hop.TokenOut
}

func routeKey(route *Route) string {
	var builder strings.Builder
	for _, hop := range route.Hops {
		builder.WriteString(hopKey(&hop))
		builder.WriteByte('\x00')
	}
	return builder.String()
}

func sumGas(values ...uint64) (uint64, bool) {
	var total uint64
	for _, value := range values {
		if value > ^uint64(0)-total {
			return 0, false
		}
		total += value
	}
	return total, true
}
