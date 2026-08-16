// Package amm provides exact-integer quoting for the on-chain liquidity that
// the CoW driver ships with each auction.
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

	maxDecimalRationalLength = 128
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
func (p *Pool) Other(token string) string {
	tokens := p.AllTokens()
	if len(tokens) != 2 {
		return ""
	}
	switch {
	case strings.EqualFold(token, tokens[0]):
		return tokens[1]
	case strings.EqualFold(token, tokens[1]):
		return tokens[0]
	default:
		return ""
	}
}

// ParseDecimalRational turns a wire decimal such as "0.003" into an exact
// rational. Exponent notation and unbounded decimal strings are rejected.
func ParseDecimalRational(value string) (*big.Int, *big.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return big.NewInt(0), big.NewInt(1), nil
	}
	if len(value) > maxDecimalRationalLength || strings.ContainsAny(value, "eE+") {
		return nil, nil, fmt.Errorf("invalid decimal %q", value)
	}
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	integerPart, fractionalPart, found := strings.Cut(value, ".")
	if !found {
		fractionalPart = ""
	}
	if integerPart == "" {
		integerPart = "0"
	}
	if !decimalDigitsOnly(integerPart) || (fractionalPart != "" && !decimalDigitsOnly(fractionalPart)) {
		return nil, nil, fmt.Errorf("invalid decimal %q", value)
	}
	digits := integerPart + fractionalPart
	numerator, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, nil, fmt.Errorf("invalid decimal %q", value)
	}
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(fractionalPart))), nil)
	if negative {
		numerator.Neg(numerator)
	}
	return numerator, denominator, nil
}

func decimalDigitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
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
	if !isU256(amountIn) || amountIn.Sign() <= 0 {
		return nil, errors.New("amountIn must be a positive uint256")
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
		inIndex, outIndex := -1, -1
		for i, token := range p.TokenList {
			switch {
			case strings.EqualFold(token, tokenIn):
				inIndex = i
			case strings.EqualFold(token, tokenOut):
				outIndex = i
			}
		}
		return p.quoteStableIndexed(inIndex, outIndex, amountIn)
	default:
		return nil, ErrUnsupportedKind
	}
}

func (p *Pool) quoteConstantProduct(tokenIn string, amountIn *big.Int) (*big.Int, error) {
	if !isU256(p.ReserveA) || !isU256(p.ReserveB) ||
		!isU256(p.FeeNum) || !isU256(p.FeeDen) || p.FeeDen.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}
	reserveIn, reserveOut := p.ReserveA, p.ReserveB
	if strings.EqualFold(tokenIn, p.TokenB) {
		reserveIn, reserveOut = p.ReserveB, p.ReserveA
	}
	if reserveIn.Sign() <= 0 || reserveOut.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}
	keep, err := subU256(p.FeeDen, p.FeeNum)
	if err != nil || keep.Sign() <= 0 {
		return nil, errors.New("invalid pool fee")
	}
	inputAfterFee, err := mulU256(amountIn, keep)
	if err != nil {
		return nil, err
	}
	numerator, err := mulU256(inputAfterFee, reserveOut)
	if err != nil {
		return nil, err
	}
	reserveTerm, err := mulU256(reserveIn, p.FeeDen)
	if err != nil {
		return nil, err
	}
	denominator, err := addU256(reserveTerm, inputAfterFee)
	if err != nil || denominator.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}
	output, err := divDownU256(numerator, denominator)
	if err != nil || output.Sign() <= 0 || output.Cmp(reserveOut) >= 0 {
		return nil, ErrNoLiquidity
	}
	return output, nil
}

// feePips converts the pool's rational fee to Uniswap-v3 pips (1e6 scale).
func (p *Pool) feePips() (*big.Int, error) {
	if !isU256(p.FeeNum) || !isU256(p.FeeDen) || p.FeeDen.Sign() <= 0 {
		return nil, errors.New("invalid concentrated pool fee")
	}
	pips := new(big.Int).Mul(p.FeeNum, big.NewInt(1_000_000))
	pips.Quo(pips, p.FeeDen)
	if !isU256(pips) || pips.Cmp(big.NewInt(1_000_000)) >= 0 {
		return nil, errors.New("invalid concentrated pool fee")
	}
	return pips, nil
}

func (p *Pool) quoteConcentrated(tokenIn string, amountIn *big.Int) (*big.Int, error) {
	if !isU256(p.SqrtPriceX96) || p.SqrtPriceX96.Cmp(minSqrtRatio) < 0 ||
		p.SqrtPriceX96.Cmp(maxSqrtRatio) > 0 || p.Liquidity == nil ||
		p.Liquidity.Sign() <= 0 || p.Liquidity.BitLen() > 128 ||
		p.Tick < MinTick || p.Tick > MaxTick {
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

	sqrtPrice := new(big.Int).Set(p.SqrtPriceX96)
	liquidity := new(big.Int).Set(p.Liquidity)
	remaining := new(big.Int).Set(amountIn)
	amountOut := new(big.Int)

	limit := new(big.Int).Sub(maxSqrtRatio, big.NewInt(1))
	if zeroForOne {
		limit = new(big.Int).Add(minSqrtRatio, big.NewInt(1))
	}

	idx := sort.Search(len(p.Ticks), func(i int) bool { return p.Ticks[i].Index > p.Tick })
	if zeroForOne {
		idx--
	}

	for steps := 0; remaining.Sign() > 0 && steps < 512; steps++ {
		if liquidity.Sign() <= 0 || liquidity.BitLen() > 128 {
			return nil, ErrNoLiquidity
		}
		target := new(big.Int).Set(limit)
		var crossing *Tick
		if zeroForOne {
			if idx >= 0 {
				tick := p.Ticks[idx]
				tickPrice, err := GetSqrtRatioAtTick(tick.Index)
				if err != nil {
					return nil, err
				}
				if tickPrice.Cmp(target) > 0 {
					target, crossing = tickPrice, &tick
				}
			}
		} else if idx < len(p.Ticks) {
			tick := p.Ticks[idx]
			tickPrice, err := GetSqrtRatioAtTick(tick.Index)
			if err != nil {
				return nil, err
			}
			if tickPrice.Cmp(target) < 0 {
				target, crossing = tickPrice, &tick
			}
		}

		next, stepIn, stepOut, fee, err := computeSwapStep(sqrtPrice, target, liquidity, remaining, pips)
		if err != nil {
			return nil, err
		}
		consumed, err := addU256(stepIn, fee)
		if err != nil {
			return nil, err
		}
		if consumed.Sign() == 0 && stepOut.Sign() == 0 && next.Cmp(sqrtPrice) == 0 && crossing == nil {
			break
		}
		if consumed.Cmp(remaining) > 0 {
			return nil, errors.New("swap step overconsumed input")
		}
		remaining.Sub(remaining, consumed)
		amountOut, err = addU256(amountOut, stepOut)
		if err != nil {
			return nil, err
		}
		sqrtPrice = next

		if crossing != nil && sqrtPrice.Cmp(target) == 0 {
			net := new(big.Int).Set(crossing.Net)
			if zeroForOne {
				net.Neg(net)
			}
			nextLiquidity := new(big.Int).Add(liquidity, net)
			if nextLiquidity.Sign() < 0 || nextLiquidity.BitLen() > 128 {
				return nil, ErrNoLiquidity
			}
			liquidity = nextLiquidity
			if zeroForOne {
				idx--
			} else {
				idx++
			}
			continue
		}
		if sqrtPrice.Cmp(limit) == 0 || (remaining.Sign() > 0 && crossing == nil) {
			break
		}
	}

	if amountOut.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}
	return amountOut, nil
}

// computeSwapStep mirrors Uniswap V3 SwapMath.computeSwapStep for exact input.
func computeSwapStep(current, target, liquidity, remaining, feePips *big.Int) (next, amountIn, amountOut, fee *big.Int, err error) {
	zeroForOne := current.Cmp(target) >= 0
	million := big.NewInt(1_000_000)
	complement, err := subU256(million, feePips)
	if err != nil || complement.Sign() <= 0 {
		return nil, nil, nil, nil, errors.New("invalid fee pips")
	}
	lessFee, err := mulDiv(remaining, complement, million)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if zeroForOne {
		amountIn, err = amount0Delta(target, current, liquidity, true)
	} else {
		amountIn, err = amount1Delta(current, target, liquidity, true)
	}
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if lessFee.Cmp(amountIn) >= 0 {
		next = new(big.Int).Set(target)
	} else {
		next, err = nextSqrtFromInput(current, liquidity, lessFee, zeroForOne)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}

	maxed := next.Cmp(target) == 0
	if zeroForOne {
		if !maxed {
			amountIn, err = amount0Delta(next, current, liquidity, true)
			if err != nil {
				return nil, nil, nil, nil, err
			}
		}
		amountOut, err = amount1Delta(next, current, liquidity, false)
	} else {
		if !maxed {
			amountIn, err = amount1Delta(current, next, liquidity, true)
			if err != nil {
				return nil, nil, nil, nil, err
			}
		}
		amountOut, err = amount0Delta(current, next, liquidity, false)
	}
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if !maxed {
		fee, err = subU256(remaining, amountIn)
	} else {
		fee, err = mulDivUp(amountIn, feePips, complement)
	}
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return next, amountIn, amountOut, fee, nil
}

func amount0Delta(a, b, liquidity *big.Int, roundUp bool) (*big.Int, error) {
	if a.Cmp(b) > 0 {
		a, b = b, a
	}
	if a.Sign() <= 0 || liquidity.Sign() < 0 || liquidity.BitLen() > 128 {
		return nil, errors.New("invalid amount0 delta input")
	}
	numerator1 := new(big.Int).Lsh(new(big.Int).Set(liquidity), 96)
	if !isU256(numerator1) {
		return nil, errU256Overflow
	}
	numerator2, err := subU256(b, a)
	if err != nil {
		return nil, err
	}
	if roundUp {
		intermediate, err := mulDivUp(numerator1, numerator2, b)
		if err != nil {
			return nil, err
		}
		return divUp(intermediate, a)
	}
	intermediate, err := mulDiv(numerator1, numerator2, b)
	if err != nil {
		return nil, err
	}
	return divDownU256(intermediate, a)
}

func amount1Delta(a, b, liquidity *big.Int, roundUp bool) (*big.Int, error) {
	if a.Cmp(b) > 0 {
		a, b = b, a
	}
	delta, err := subU256(b, a)
	if err != nil {
		return nil, err
	}
	if roundUp {
		return mulDivUp(liquidity, delta, q96)
	}
	return mulDiv(liquidity, delta, q96)
}

func nextSqrtFromInput(sqrtPrice, liquidity, amount *big.Int, zeroForOne bool) (*big.Int, error) {
	if amount.Sign() == 0 {
		return new(big.Int).Set(sqrtPrice), nil
	}
	if liquidity.Sign() <= 0 || liquidity.BitLen() > 128 {
		return nil, errors.New("invalid liquidity")
	}
	if zeroForOne {
		numerator := new(big.Int).Lsh(new(big.Int).Set(liquidity), 96)
		if !isU256(numerator) {
			return nil, errU256Overflow
		}
		product := new(big.Int).Mul(amount, sqrtPrice)
		if product.Cmp(maxU256) <= 0 {
			denominator := new(big.Int).Add(numerator, product)
			if denominator.Cmp(maxU256) <= 0 && denominator.Cmp(numerator) >= 0 {
				return mulDivUp(numerator, sqrtPrice, denominator)
			}
		}
		base, err := divDownU256(numerator, sqrtPrice)
		if err != nil {
			return nil, err
		}
		denominator, err := addU256(base, amount)
		if err != nil {
			return nil, err
		}
		return divUp(numerator, denominator)
	}
	quotient, err := mulDiv(amount, q96, liquidity)
	if err != nil {
		return nil, err
	}
	next, err := addU256(sqrtPrice, quotient)
	if err != nil || next.Cmp(maxU160) > 0 {
		return nil, errors.New("sqrt price overflow")
	}
	return next, nil
}

// GetSqrtRatioAtTick is Uniswap V3 TickMath.getSqrtRatioAtTick.
func GetSqrtRatioAtTick(tick int32) (*big.Int, error) {
	if tick < MinTick || tick > MaxTick {
		return nil, errors.New("tick out of range")
	}
	absolute := int64(tick)
	if absolute < 0 {
		absolute = -absolute
	}
	var ratio *big.Int
	if absolute&1 != 0 {
		ratio, _ = new(big.Int).SetString(tickFactors[0], 16)
	} else {
		ratio = new(big.Int).Set(two128)
	}
	for i := 1; i < len(tickFactors); i++ {
		if absolute&(1<<uint(i)) != 0 {
			factor, _ := new(big.Int).SetString(tickFactors[i], 16)
			ratio.Mul(ratio, factor)
			ratio.Rsh(ratio, 128)
		}
	}
	if tick > 0 {
		ratio.Quo(new(big.Int).Set(maxU256), ratio)
	}
	quotient, remainder := new(big.Int).QuoRem(ratio, new(big.Int).Lsh(big.NewInt(1), 32), new(big.Int))
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if quotient.Cmp(maxU160) > 0 {
		return nil, errors.New("sqrt ratio overflow")
	}
	return quotient, nil
}

// mulDiv performs full-precision multiplication followed by division and then
// requires the quotient to fit uint256, matching Uniswap FullMath behavior.
func mulDiv(a, b, denominator *big.Int) (*big.Int, error) {
	if !isU256(a) || !isU256(b) || !isU256(denominator) || denominator.Sign() <= 0 {
		return nil, errors.New("invalid mulDiv input")
	}
	quotient := new(big.Int).Quo(new(big.Int).Mul(a, b), denominator)
	if !isU256(quotient) {
		return nil, errU256Overflow
	}
	return quotient, nil
}

func mulDivUp(a, b, denominator *big.Int) (*big.Int, error) {
	if !isU256(a) || !isU256(b) || !isU256(denominator) || denominator.Sign() <= 0 {
		return nil, errors.New("invalid mulDiv input")
	}
	product := new(big.Int).Mul(a, b)
	quotient, remainder := new(big.Int).QuoRem(product, denominator, new(big.Int))
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !isU256(quotient) {
		return nil, errU256Overflow
	}
	return quotient, nil
}

func divUp(a, denominator *big.Int) (*big.Int, error) {
	return divUpU256(a, denominator)
}

// SortTicks orders ticks ascending, as the swap loop requires.
func SortTicks(ticks []Tick) {
	sort.Slice(ticks, func(i, j int) bool { return ticks[i].Index < ticks[j].Index })
}
