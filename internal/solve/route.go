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
// given input amount, searching direct pools then two-hop paths.
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
		cand := &Route{
			Hops: []Hop{{Pool: p, TokenIn: sellToken, TokenOut: buyToken, AmountIn: amountIn, Out: out}},
			Out:  out,
			Gas:  p.GasEstimate,
		}
		best = better(best, cand)
	}

	// --- two hop ---
	for _, mid := range g.intermediates {
		if mid == sellToken || mid == buyToken {
			continue
		}
		var leg1 *Hop
		for _, p := range g.poolsFor(sellToken) {
			if !p.Supports(sellToken, mid) {
				continue
			}
			out, err := p.QuoteExactInPair(sellToken, mid, amountIn)
			if err != nil {
				continue
			}
			candidate := &Hop{Pool: p, TokenIn: sellToken, TokenOut: mid, AmountIn: amountIn, Out: out}
			if betterHop(leg1, candidate) == candidate {
				leg1 = candidate
			}
		}
		if leg1 == nil {
			continue
		}
		var leg2 *Hop
		for _, p := range g.poolsFor(mid) {
			// Quoting the same stateful pool twice without applying the first
			// swap's state transition is not a valid route simulation.
			if p == leg1.Pool || !p.Supports(mid, buyToken) {
				continue
			}
			out, err := p.QuoteExactInPair(mid, buyToken, leg1.Out)
			if err != nil {
				continue
			}
			candidate := &Hop{Pool: p, TokenIn: mid, TokenOut: buyToken, AmountIn: leg1.Out, Out: out}
			if betterHop(leg2, candidate) == candidate {
				leg2 = candidate
			}
		}
		if leg2 == nil {
			continue
		}
		cand := &Route{
			Hops: []Hop{*leg1, *leg2},
			Out:  leg2.Out,
			Gas:  leg1.Pool.GasEstimate + leg2.Pool.GasEstimate,
		}
		best = better(best, cand)
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

func betterHop(a, b *Hop) *Hop {
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
	if hopKey(b) < hopKey(a) {
		return b
	}
	return a
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
