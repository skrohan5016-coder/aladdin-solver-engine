package solve

import (
	"math/big"
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
	// many pools reference them (deepest liquidity first).
	intermediates []string
}

func NewGraph(pools []*amm.Pool) *Graph {
	g := &Graph{byToken: map[string][]*amm.Pool{}}
	count := map[string]int{}
	for _, p := range pools {
		a, b := strings.ToLower(p.TokenA), strings.ToLower(p.TokenB)
		g.byToken[a] = append(g.byToken[a], p)
		g.byToken[b] = append(g.byToken[b], p)
		count[a]++
		count[b]++
	}
	for tok, n := range count {
		if n >= 2 {
			g.intermediates = append(g.intermediates, tok)
		}
	}
	// Most-connected tokens first; these are the real base assets of the
	// auction (WETH, USDC, ...) without hardcoding any address.
	for i := 0; i < len(g.intermediates); i++ {
		for j := i + 1; j < len(g.intermediates); j++ {
			if count[g.intermediates[j]] > count[g.intermediates[i]] {
				g.intermediates[i], g.intermediates[j] = g.intermediates[j], g.intermediates[i]
			}
		}
	}
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
		if !strings.EqualFold(p.Other(sellToken), buyToken) {
			continue
		}
		out, err := p.QuoteExactIn(sellToken, amountIn)
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
			if !strings.EqualFold(p.Other(sellToken), mid) {
				continue
			}
			out, err := p.QuoteExactIn(sellToken, amountIn)
			if err != nil {
				continue
			}
			if leg1 == nil || out.Cmp(leg1.Out) > 0 {
				leg1 = &Hop{Pool: p, TokenIn: sellToken, TokenOut: mid, AmountIn: amountIn, Out: out}
			}
		}
		if leg1 == nil {
			continue
		}
		var leg2 *Hop
		for _, p := range g.poolsFor(mid) {
			if !strings.EqualFold(p.Other(mid), buyToken) {
				continue
			}
			out, err := p.QuoteExactIn(mid, leg1.Out)
			if err != nil {
				continue
			}
			if leg2 == nil || out.Cmp(leg2.Out) > 0 {
				leg2 = &Hop{Pool: p, TokenIn: mid, TokenOut: buyToken, AmountIn: leg1.Out, Out: out}
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

func (g *Graph) poolsFor(tok string) []*amm.Pool {
	ps := g.byToken[tok]
	if len(ps) > maxPoolsPerToken {
		return ps[:maxPoolsPerToken]
	}
	return ps
}

func better(a, b *Route) *Route {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if b.Out.Cmp(a.Out) > 0 {
		return b
	}
	return a
}
