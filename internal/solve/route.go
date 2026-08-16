package solve

import (
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
	g := &Graph{byToken: map[string][]*amm.Pool{}}
	count := map[string]int{}
	for _, p := range pools {
		for _, token := range p.AllTokens() {
			token = strings.ToLower(token)
			g.byToken[token] = append(g.byToken[token], p)
			count[token]++
		}
	}
	for token, n := range count {
		if n >= 2 {
			g.intermediates = append(g.intermediates, token)
		}
	}
	// Map iteration is deliberately removed from the final ordering. Equal
	// connectivity is broken lexicographically so replay is deterministic.
	sort.Slice(g.intermediates, func(i, j int) bool {
		left, right := g.intermediates[i], g.intermediates[j]
		if count[left] != count[right] {
			return count[left] > count[right]
		}
		return left < right
	})
	if len(g.intermediates) > maxIntermediates {
		g.intermediates = g.intermediates[:maxIntermediates]
	}
	return g
}

const (
	maxIntermediates = 12
	maxPoolsPerToken = 24
)

// BestRoute finds the highest-output path from sellToken to buyToken for the
// given input amount, searching direct pools then every bounded two-hop pair.
func (g *Graph) BestRoute(sellToken, buyToken string, amountIn *big.Int) *Route {
	sellToken, buyToken = strings.ToLower(sellToken), strings.ToLower(buyToken)
	if sellToken == buyToken || amountIn == nil || amountIn.Sign() <= 0 {
		return nil
	}
	var best *Route

	// --- direct ---
	for _, p := range g.poolsFor(sellToken) {
		if !p.Supports(sellToken, buyToken) {
			continue
		}
		out, err := p.QuoteExactInPair(sellToken, buyToken, amountIn)
		if err != nil {
			continue
		}
		best = better(best, &Route{
			Hops: []Hop{{Pool: p, TokenIn: sellToken, TokenOut: buyToken, AmountIn: amountIn, Out: out}},
			Out:  out,
			Gas:  p.GasEstimate,
		})
	}

	// --- two hop ---
	for _, mid := range g.intermediates {
		if mid == sellToken || mid == buyToken {
			continue
		}
		for _, first := range g.poolsFor(sellToken) {
			if !first.Supports(sellToken, mid) {
				continue
			}
			midAmount, err := first.QuoteExactInPair(sellToken, mid, amountIn)
			if err != nil {
				continue
			}
			for _, second := range g.poolsFor(mid) {
				// Reusing one stateful pool would require applying the first swap's
				// reserve/tick transition before quoting the second.
				if second == first || !second.Supports(mid, buyToken) {
					continue
				}
				out, err := second.QuoteExactInPair(mid, buyToken, midAmount)
				if err != nil {
					continue
				}
				best = better(best, &Route{
					Hops: []Hop{
						{Pool: first, TokenIn: sellToken, TokenOut: mid, AmountIn: amountIn, Out: midAmount},
						{Pool: second, TokenIn: mid, TokenOut: buyToken, AmountIn: midAmount, Out: out},
					},
					Out: out,
					Gas: first.GasEstimate + second.GasEstimate,
				})
			}
		}
	}
	return best
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

func hopKey(h *Hop) string {
	return h.Pool.Kind + "\x00" + h.Pool.ID + "\x00" + h.TokenIn + "\x00" + h.TokenOut
}

func routeKey(route *Route) string {
	var b strings.Builder
	for _, hop := range route.Hops {
		b.WriteString(hopKey(&hop))
		b.WriteByte('\x00')
	}
	return b.String()
}
