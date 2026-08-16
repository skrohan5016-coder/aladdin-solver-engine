// Package amm provides exact-integer quoting for the on-chain liquidity that
// the CoW driver ships with each auction.
//
// Rule for this package: no float64 ever touches a token amount. Decimal fee
// strings from the wire ("0.003") are parsed into exact rationals.
package amm

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

var (
	ErrUnsupportedKind = errors.New("unsupported liquidity kind")
	ErrNoLiquidity     = errors.New("insufficient liquidity")

	maxU256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	maxU160 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 160), big.NewInt(1))
	q96     = new(big.Int).Lsh(big.NewInt(1), 96)
	two128  = new(big.Int).Lsh(big.NewInt(1), 128)

	minSqrtRatio, _ = new(big.Int).SetString("4295128739", 10)
	maxSqrtRatio, _ = new(big.Int).SetString("1461446703485210103287273052203988822378723970342", 10)

	tickFactors = []string{
		"fffcb933bd6fad37aa2d162d1a594001", "fff97272373d413259a46990580e213a",
		"fff2e50f5f656932ef12357cf3c7fdcc", "ffe5caca7e10e4e61c3624eaa0941cd0",
		"ffcb9843d60f6159c9db58835c926644", "ff973b41fa98c081472e6896dfb254c0",
		"ff2ea16466c96a3843ec78b326b52861", "fe5dee046a99a2a811c461f1969c3053",
		"fcbe86c7900a88aedcffc83b479aa3a4", "f987a7253ac413176f2b074cf7815e54",
		"f3392b0822b70005940c7a398e4b70f3", "e7159475a2c29b7443b29c7fa6e889d9",
		"d097f3bdfd2022b8845ad8f792aa5825", "a9f746462d870fdf8a65dc1f90e061e5",
		"70d869a156d2a1b890bb3df62baf32f7", "31be135f97d08fd981231505542fcfa6",
		"9aa508b5b7a84e1c677de54f3e99bc9", "5d6af8dedb81196699c329225ee604",
		"2216e584f5fa1ea926041bedfe98", "48a170391f7dc42444e8fa2",
	}
)

const (
	MinTick int32 = -887272
	MaxTick int32 = 887272
)

// Pool is the routable form of one liquidity source from the auction.
type Pool struct {
	ID          string
	Kind        string
	Address     string
	GasEstimate uint64

	TokenA, TokenB string

	// constant product
	ReserveA, ReserveB *big.Int

	// fee as an exact rational feeNum/feeDen
	FeeNum, FeeDen *big.Int

	// concentrated liquidity
	SqrtPriceX96 *big.Int
	Liquidity    *big.Int
	Tick         int32
	Ticks        []Tick // sorted ascending by Index

	// stable liquidity. TokenList, Balances and ScalingFactors share indices.
	TokenList        []string
	Balances         []*big.Int
	ScalingFactors   []*big.Int
	AmplificationRaw *big.Int
}

type Tick struct {
	Index int32
	Net   *big.Int
}

// Tokens returns the legacy two-token view of a pool. Multi-token stable pools
// should use AllTokens instead.
func (p *Pool) Tokens() (string, string) { return p.TokenA, p.TokenB }

// AllTokens returns every token address in deterministic order.
func (p *Pool) AllTokens() []string {
	if len(p.TokenList) > 0 {
		out := make([]string, len(p.TokenList))
		for i, token := range p.TokenList {
			out[i] = strings.ToLower(token)
		}
		return out
	}

	out := make([]string, 0, 2)
	if p.TokenA != "" {
		out = append(out, strings.ToLower(p.TokenA))
	}
	if p.TokenB != "" && !strings.EqualFold(p.TokenA, p.TokenB) {
		out = append(out, strings.ToLower(p.TokenB))
	}
	return out
}

// Supports reports whether the pool can quote the requested ordered pair.
func (p *Pool) Supports(tokenIn, tokenOut string) bool {
	if strings.EqualFold(tokenIn, tokenOut) {
		return false
	}
	in, out := false, false
	for _, token := range p.AllTokens() {
		if strings.EqualFold(token, tokenIn) {
			in = true
		}
		if strings.EqualFold(token, tokenOut) {
			out = true
		}
	}
	return in && out
}

// Other returns the counterpart token for a two-token pool, or "" when the
// token is absent or the pool has more than two tokens.
func (p *Pool) Other(tok string) string {
	tokens := p.AllTokens()
	if len(tokens) != 2 {
		return ""
	}
	switch {
	case strings.EqualFold(tok, tokens[0]):
		return tokens[1]
	case strings.EqualFold(tok, tokens[1]):
		return tokens[0]
	default:
		return ""
	}
}

// ParseDecimalRational turns a wire decimal such as "0.003" into an exact
// rational. It rejects exponent notation and anything non-numeric.
func ParseDecimalRational(s string) (*big.Int, *big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return big.NewInt(0), big.NewInt(1), nil
	}
	if strings.ContainsAny(s, "eE") {
		return nil, nil, fmt.Errorf("exponent notation not accepted: %q", s)
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, fracPart, _ := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	digits := intPart + fracPart
	num, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, nil, fmt.Errorf("invalid decimal %q", s)
	}
	den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(fracPart))), nil)
	if neg {
		num.Neg(num)
	}
	return num, den, nil
}

// QuoteExactIn quotes a two-token pool. Multi-token pools require
// QuoteExactInPair so the output token is unambiguous.
func (p *Pool) QuoteExactIn(tokenIn string, amountIn *big.Int) (*big.Int, error) {
	tokenOut := p.Other(tokenIn)
	if tokenOut == "" {
		for _, token := range p.AllTokens() {
			if strings.EqualFold(token, tokenIn) {
				return nil, errors.New("multi-token pool needs an explicit output token")
			}
		}
		return nil, errors.New("token not in pool")
	}
	return p.QuoteExactInPair(tokenIn, tokenOut, amountIn)
}

// QuoteExactInPair returns the output amount for a specific ordered token pair.
func (p *Pool) QuoteExactInPair(tokenIn, tokenOut string, amountIn *big.Int) (*big.Int, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, errors.New("amountIn must be positive")
	}
	if !p.Supports(tokenIn, tokenOut) {
		return nil, errors.New("token pair not in pool")
	}

	tokenIn = strings.ToLower(tokenIn)
	tokenOut = strings.ToLower(tokenOut)
	switch p.Kind {
	case "constantProduct":
		return p.quoteConstantProduct(tokenIn, amountIn)
	case "concentratedLiquidity":
		return p.quoteConcentrated(tokenIn, amountIn)
	case "stable":
		inIdx, outIdx := -1, -1
		for i, token := range p.TokenList {
			switch {
			case strings.EqualFold(token, tokenIn):
				inIdx = i
			case strings.EqualFold(token, tokenOut):
				outIdx = i
			}
		}
		return p.quoteStableIndexed(inIdx, outIdx, amountIn)
	default:
		return nil, ErrUnsupportedKind
	}
}

func (p *Pool) quoteConstantProduct(tokenIn string, amountIn *big.Int) (*big.Int, error) {
	if p.ReserveA == nil || p.ReserveB == nil || p.FeeNum == nil || p.FeeDen == nil || p.FeeDen.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}
	rIn, rOut := p.ReserveA, p.ReserveB
	if strings.EqualFold(tokenIn, p.TokenB) {
		rIn, rOut = p.ReserveB, p.ReserveA
	}
	if rIn.Sign() <= 0 || rOut.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}
	// amountInAfterFee = amountIn * (feeDen - feeNum) / feeDen, kept as a
	// numerator over feeDen so nothing is rounded before the final division.
	keep := new(big.Int).Sub(p.FeeDen, p.FeeNum)
	if p.FeeNum.Sign() < 0 || keep.Sign() <= 0 {
		return nil, errors.New("invalid pool fee")
	}
	inAfter := new(big.Int).Mul(amountIn, keep)
	num := new(big.Int).Mul(inAfter, rOut)
	den := new(big.Int).Add(new(big.Int).Mul(rIn, p.FeeDen), inAfter)
	if den.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}
	out := new(big.Int).Quo(num, den)
	if out.Sign() <= 0 || out.Cmp(rOut) >= 0 {
		return nil, ErrNoLiquidity
	}
	return out, nil
}

// feePips converts the pool's rational fee to Uniswap-v3 pips (1e6 scale).
func (p *Pool) feePips() (*big.Int, error) {
	if p.FeeNum == nil || p.FeeDen == nil || p.FeeDen.Sign() <= 0 || p.FeeNum.Sign() < 0 {
		return nil, errors.New("invalid concentrated pool fee")
	}
	pips := new(big.Int).Mul(p.FeeNum, big.NewInt(1_000_000))
	pips.Quo(pips, p.FeeDen)
	if pips.Sign() < 0 || pips.Cmp(big.NewInt(1_000_000)) >= 0 {
		return nil, errors.New("invalid concentrated pool fee")
	}
	return pips, nil
}

func (p *Pool) quoteConcentrated(tokenIn string, amountIn *big.Int) (*big.Int, error) {
	if p.SqrtPriceX96 == nil || p.Liquidity == nil {
		return nil, ErrNoLiquidity
	}
	// token0 is the lexicographically smaller address, matching Uniswap.
	token0 := p.TokenA
	if strings.ToLower(p.TokenB) < strings.ToLower(p.TokenA) {
		token0 = p.TokenB
	}
	zeroForOne := strings.EqualFold(tokenIn, token0)

	pips, err := p.feePips()
	if err != nil {
		return nil, err
	}

	sqrtP := new(big.Int).Set(p.SqrtPriceX96)
	liq := new(big.Int).Set(p.Liquidity)
	remaining := new(big.Int).Set(amountIn)
	amountOut := new(big.Int)

	limit := new(big.Int).Sub(maxSqrtRatio, big.NewInt(1))
	if zeroForOne {
		limit = new(big.Int).Add(minSqrtRatio, big.NewInt(1))
	}

	// Walk the initialized ticks in the direction of the swap.
	idx := sort.Search(len(p.Ticks), func(i int) bool { return p.Ticks[i].Index > p.Tick })
	if zeroForOne {
		idx--
	}

	for steps := 0; remaining.Sign() > 0 && steps < 512; steps++ {
		if liq.Sign() <= 0 {
			return nil, ErrNoLiquidity
		}
		target := new(big.Int).Set(limit)
		var crossing *Tick
		if zeroForOne {
			if idx >= 0 {
				t := p.Ticks[idx]
				st, err := GetSqrtRatioAtTick(t.Index)
				if err != nil {
					return nil, err
				}
				if st.Cmp(target) > 0 {
					target, crossing = st, &t
				}
			}
		} else {
			if idx < len(p.Ticks) {
				t := p.Ticks[idx]
				st, err := GetSqrtRatioAtTick(t.Index)
				if err != nil {
					return nil, err
				}
				if st.Cmp(target) < 0 {
					target, crossing = st, &t
				}
			}
		}

		next, stepIn, stepOut, fee, err := computeSwapStep(sqrtP, target, liq, remaining, pips)
		if err != nil {
			return nil, err
		}
		consumed := new(big.Int).Add(stepIn, fee)
		if consumed.Sign() == 0 && stepOut.Sign() == 0 && next.Cmp(sqrtP) == 0 && crossing == nil {
			break
		}
		if consumed.Cmp(remaining) > 0 {
			return nil, errors.New("swap step overconsumed input")
		}
		remaining.Sub(remaining, consumed)
		amountOut.Add(amountOut, stepOut)
		sqrtP = next

		if crossing != nil && sqrtP.Cmp(target) == 0 {
			net := new(big.Int).Set(crossing.Net)
			if zeroForOne {
				net.Neg(net)
			}
			liq.Add(liq, net)
			if liq.Sign() < 0 {
				return nil, ErrNoLiquidity
			}
			if zeroForOne {
				idx--
			} else {
				idx++
			}
			continue
		}
		if sqrtP.Cmp(limit) == 0 {
			break
		}
		if remaining.Sign() > 0 && crossing == nil {
			break
		}
	}

	if amountOut.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}
	return amountOut, nil
}

// computeSwapStep mirrors Uniswap V3 SwapMath.computeSwapStep for exact input.
func computeSwapStep(cur, target, liq, remaining, feePips *big.Int) (next, amtIn, amtOut, fee *big.Int, err error) {
	zeroForOne := cur.Cmp(target) >= 0
	million := big.NewInt(1_000_000)
	comp := new(big.Int).Sub(million, feePips)

	lessFee := new(big.Int).Quo(new(big.Int).Mul(remaining, comp), million)

	if zeroForOne {
		amtIn, err = amount0Delta(target, cur, liq, true)
	} else {
		amtIn, err = amount1Delta(cur, target, liq, true)
	}
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if lessFee.Cmp(amtIn) >= 0 {
		next = new(big.Int).Set(target)
	} else {
		next, err = nextSqrtFromInput(cur, liq, lessFee, zeroForOne)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}

	maxed := next.Cmp(target) == 0
	if zeroForOne {
		if !maxed {
			if amtIn, err = amount0Delta(next, cur, liq, true); err != nil {
				return nil, nil, nil, nil, err
			}
		}
		if amtOut, err = amount1Delta(next, cur, liq, false); err != nil {
			return nil, nil, nil, nil, err
		}
	} else {
		if !maxed {
			if amtIn, err = amount1Delta(cur, next, liq, true); err != nil {
				return nil, nil, nil, nil, err
			}
		}
		if amtOut, err = amount0Delta(cur, next, liq, false); err != nil {
			return nil, nil, nil, nil, err
		}
	}

	if !maxed {
		fee = new(big.Int).Sub(remaining, amtIn)
	} else {
		fee, err = mulDivUp(amtIn, feePips, comp)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}
	return next, amtIn, amtOut, fee, nil
}

func amount0Delta(a, b, l *big.Int, roundUp bool) (*big.Int, error) {
	if a.Cmp(b) > 0 {
		a, b = b, a
	}
	if a.Sign() <= 0 {
		return nil, errors.New("zero sqrt price")
	}
	n1 := new(big.Int).Lsh(new(big.Int).Set(l), 96)
	n2 := new(big.Int).Sub(b, a)
	if roundUp {
		x, err := mulDivUp(n1, n2, b)
		if err != nil {
			return nil, err
		}
		return divUp(x, a)
	}
	x, err := mulDiv(n1, n2, b)
	if err != nil {
		return nil, err
	}
	return new(big.Int).Quo(x, a), nil
}

func amount1Delta(a, b, l *big.Int, roundUp bool) (*big.Int, error) {
	if a.Cmp(b) > 0 {
		a, b = b, a
	}
	d := new(big.Int).Sub(b, a)
	if roundUp {
		return mulDivUp(l, d, q96)
	}
	return mulDiv(l, d, q96)
}

func nextSqrtFromInput(sqrtP, liq, amt *big.Int, zeroForOne bool) (*big.Int, error) {
	if amt.Sign() == 0 {
		return new(big.Int).Set(sqrtP), nil
	}
	if liq.Sign() <= 0 {
		return nil, errors.New("zero liquidity")
	}
	if zeroForOne {
		// adding token0
		n := new(big.Int).Lsh(new(big.Int).Set(liq), 96)
		prod := new(big.Int).Mul(amt, sqrtP)
		if prod.Cmp(maxU256) <= 0 {
			den := new(big.Int).Add(n, prod)
			if den.Cmp(maxU256) <= 0 && den.Cmp(n) >= 0 {
				return mulDivUp(n, sqrtP, den)
			}
		}
		den := new(big.Int).Add(new(big.Int).Quo(n, sqrtP), amt)
		return divUp(n, den)
	}
	// adding token1
	q, err := mulDiv(amt, q96, liq)
	if err != nil {
		return nil, err
	}
	x := new(big.Int).Add(sqrtP, q)
	if x.Cmp(maxU160) > 0 {
		return nil, errors.New("sqrt price overflow")
	}
	return x, nil
}

// GetSqrtRatioAtTick is Uniswap V3 TickMath.getSqrtRatioAtTick.
func GetSqrtRatioAtTick(tick int32) (*big.Int, error) {
	if tick < MinTick || tick > MaxTick {
		return nil, errors.New("tick out of range")
	}
	a := int64(tick)
	if a < 0 {
		a = -a
	}
	var r *big.Int
	if a&1 != 0 {
		r, _ = new(big.Int).SetString(tickFactors[0], 16)
	} else {
		r = new(big.Int).Set(two128)
	}
	for i := 1; i < len(tickFactors); i++ {
		if a&(1<<uint(i)) != 0 {
			f, _ := new(big.Int).SetString(tickFactors[i], 16)
			r.Mul(r, f)
			r.Rsh(r, 128)
		}
	}
	if tick > 0 {
		r.Quo(new(big.Int).Set(maxU256), r)
	}
	q, rem := new(big.Int).QuoRem(r, new(big.Int).Lsh(big.NewInt(1), 32), new(big.Int))
	if rem.Sign() != 0 {
		q.Add(q, big.NewInt(1))
	}
	if q.Cmp(maxU160) > 0 {
		return nil, errors.New("sqrt ratio overflow")
	}
	return q, nil
}

func mulDiv(a, b, d *big.Int) (*big.Int, error) {
	if d == nil || d.Sign() <= 0 {
		return nil, errors.New("division by zero")
	}
	return new(big.Int).Quo(new(big.Int).Mul(a, b), d), nil
}

func mulDivUp(a, b, d *big.Int) (*big.Int, error) {
	if d == nil || d.Sign() <= 0 {
		return nil, errors.New("division by zero")
	}
	prod := new(big.Int).Mul(a, b)
	q, r := new(big.Int).QuoRem(prod, d, new(big.Int))
	if r.Sign() != 0 {
		q.Add(q, big.NewInt(1))
	}
	return q, nil
}

func divUp(a, b *big.Int) (*big.Int, error) {
	if b == nil || b.Sign() <= 0 {
		return nil, errors.New("division by zero")
	}
	q, r := new(big.Int).QuoRem(a, b, new(big.Int))
	if r.Sign() != 0 {
		q.Add(q, big.NewInt(1))
	}
	return q, nil
}

// SortTicks orders ticks ascending, as the swap loop requires.
func SortTicks(t []Tick) { sort.Slice(t, func(i, j int) bool { return t[i].Index < t[j].Index }) }
