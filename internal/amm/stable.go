package amm

import (
	"errors"
	"math/big"
)

// Balancer StableMath, ported to exact integer arithmetic.
//
// This mirrors StableMath.sol as used by Balancer V2 stable pools and the
// Curve-like pools the CoW driver reports as kind "stable". The reference is
// cowprotocol/services crates/liquidity-sources/src/balancer_v2/swap/stable_math.rs,
// which in turn mirrors the deployed contract.
//
// Two conventions matter and are easy to get wrong:
//
//   - The amplification parameter arrives on the wire as a plain decimal (100),
//     but the math wants it pre-multiplied by AMP_PRECISION (100000).
//   - Balances are upscaled to 18 decimals with the pool's scaling factor
//     before any math, and the output is downscaled afterwards. Scaling factors
//     and fees are themselves 18-decimal fixed point.
//
// Inside the invariant and balance solvers the arithmetic is plain integer
// mul/div (Solidity's Math.mul / Math.divDown), not fixed point.

var (
	ampPrecision = big.NewInt(1000)
	fpOne        = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

	ErrStableNoConverge = errors.New("stable math did not converge")
)

const stableMaxIterations = 255

// quoteStable returns the output amount for selling amountIn of tokenIn.
func (p *Pool) quoteStable(tokenIn string, amountIn *big.Int) (*big.Int, error) {
	inIdx, outIdx := -1, -1
	for i, t := range p.TokenList {
		if t == tokenIn {
			inIdx = i
		}
	}
	if inIdx < 0 {
		return nil, errors.New("token not in pool")
	}
	// A stable pool may hold more than two tokens; the caller resolves which
	// counterpart it wants via QuoteExactInPair. Default to the single other
	// token only when the pool really is a pair.
	if len(p.TokenList) != 2 {
		return nil, errors.New("stable pool needs an explicit output token")
	}
	outIdx = 1 - inIdx
	return p.quoteStableIndexed(inIdx, outIdx, amountIn)
}

func (p *Pool) quoteStableIndexed(inIdx, outIdx int, amountIn *big.Int) (*big.Int, error) {
	n := len(p.TokenList)
	if n < 2 || len(p.Balances) != n || len(p.ScalingFactors) != n {
		return nil, ErrNoLiquidity
	}
	if inIdx == outIdx || inIdx < 0 || outIdx < 0 || inIdx >= n || outIdx >= n {
		return nil, errors.New("bad token index")
	}
	if p.AmplificationRaw == nil || p.AmplificationRaw.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}

	// Fee is charged on the input, before scaling, rounding the fee up.
	feeBfp := p.feeBfp()
	if feeBfp.Cmp(fpOne) >= 0 {
		return nil, errors.New("invalid pool fee")
	}
	amountAfterFee := new(big.Int).Sub(amountIn, fpMulUp(amountIn, feeBfp))
	if amountAfterFee.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}

	// Upscale every balance and the input to 18 decimals.
	balances := make([]*big.Int, n)
	for i := range balances {
		if p.Balances[i].Sign() <= 0 {
			return nil, ErrNoLiquidity
		}
		balances[i] = fpMulDown(p.Balances[i], p.ScalingFactors[i])
		if balances[i].Sign() <= 0 {
			return nil, ErrNoLiquidity
		}
	}
	upIn := fpMulDown(amountAfterFee, p.ScalingFactors[inIdx])
	if upIn.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}

	invariant, err := stableInvariant(p.AmplificationRaw, balances)
	if err != nil {
		return nil, err
	}
	if invariant.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}

	balances[inIdx] = new(big.Int).Add(balances[inIdx], upIn)
	finalOut, err := stableBalanceGivenInvariant(p.AmplificationRaw, balances, invariant, outIdx)
	if err != nil {
		return nil, err
	}
	balances[inIdx] = new(big.Int).Sub(balances[inIdx], upIn)

	upOut := new(big.Int).Sub(balances[outIdx], finalOut)
	upOut.Sub(upOut, big.NewInt(1)) // the contract keeps one wei
	if upOut.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}

	// Downscale the output, rounding down in the user's disfavour as the
	// contract does.
	out := fpDivDown(upOut, p.ScalingFactors[outIdx])
	if out.Sign() <= 0 || out.Cmp(p.Balances[outIdx]) >= 0 {
		return nil, ErrNoLiquidity
	}
	return out, nil
}

// stableInvariant solves for D by Newton iteration.
func stableInvariant(ampTimesPrecision *big.Int, balances []*big.Int) (*big.Int, error) {
	n := int64(len(balances))
	nBig := big.NewInt(n)

	sum := new(big.Int)
	for _, b := range balances {
		sum.Add(sum, b)
	}
	if sum.Sign() == 0 {
		return new(big.Int), nil
	}

	invariant := new(big.Int).Set(sum)
	ampTimesTotal := new(big.Int).Mul(ampTimesPrecision, nBig)

	for i := 0; i < stableMaxIterations; i++ {
		dP := new(big.Int).Set(invariant)
		for _, b := range balances {
			den := new(big.Int).Mul(b, nBig)
			if den.Sign() == 0 {
				return nil, ErrNoLiquidity
			}
			dP = new(big.Int).Quo(new(big.Int).Mul(dP, invariant), den)
		}
		prev := new(big.Int).Set(invariant)

		// ((ampTimesTotal * sum) / AMP_PRECISION + dP * n) * invariant
		numerator := new(big.Int).Quo(new(big.Int).Mul(ampTimesTotal, sum), ampPrecision)
		numerator.Add(numerator, new(big.Int).Mul(dP, nBig))
		numerator.Mul(numerator, invariant)

		// ((ampTimesTotal - AMP_PRECISION) * invariant) / AMP_PRECISION + (n+1) * dP
		denominator := new(big.Int).Sub(ampTimesTotal, ampPrecision)
		denominator.Mul(denominator, invariant)
		denominator.Quo(denominator, ampPrecision)
		denominator.Add(denominator, new(big.Int).Mul(big.NewInt(n+1), dP))
		if denominator.Sign() <= 0 {
			return nil, ErrNoLiquidity
		}

		invariant = new(big.Int).Quo(numerator, denominator)
		if converged(invariant, prev) {
			return invariant, nil
		}
	}
	return nil, ErrStableNoConverge
}

// stableBalanceGivenInvariant solves for one token's balance holding D fixed.
func stableBalanceGivenInvariant(ampTimesPrecision *big.Int, balances []*big.Int, invariant *big.Int, tokenIndex int) (*big.Int, error) {
	n := int64(len(balances))
	nBig := big.NewInt(n)
	ampTimesTotal := new(big.Int).Mul(ampTimesPrecision, nBig)

	sum := new(big.Int).Set(balances[0])
	pD := new(big.Int).Mul(sum, nBig)
	for _, b := range balances[1:] {
		pD = new(big.Int).Mul(pD, b)
		pD.Mul(pD, nBig)
		if invariant.Sign() == 0 {
			return nil, ErrNoLiquidity
		}
		pD.Quo(pD, invariant)
		sum.Add(sum, b)
	}
	sum.Sub(sum, balances[tokenIndex])

	inv2 := new(big.Int).Mul(invariant, invariant)

	den := new(big.Int).Mul(ampTimesTotal, pD)
	if den.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}
	c := divUpInt(inv2, den)
	c.Mul(c, ampPrecision)
	c.Mul(c, balances[tokenIndex])

	b := new(big.Int).Quo(invariant, ampTimesTotal)
	b.Mul(b, ampPrecision)
	b.Add(b, sum)

	tokenBalance := divUpInt(new(big.Int).Add(inv2, c), new(big.Int).Add(invariant, b))
	for i := 0; i < stableMaxIterations; i++ {
		prev := new(big.Int).Set(tokenBalance)

		num := new(big.Int).Mul(tokenBalance, tokenBalance)
		num.Add(num, c)

		d := new(big.Int).Mul(tokenBalance, big.NewInt(2))
		d.Add(d, b)
		d.Sub(d, invariant)
		if d.Sign() <= 0 {
			return nil, ErrNoLiquidity
		}

		tokenBalance = divUpInt(num, d)
		if converged(tokenBalance, prev) {
			return tokenBalance, nil
		}
	}
	return nil, ErrStableNoConverge
}

func converged(cur, prev *big.Int) bool {
	d := new(big.Int).Sub(cur, prev)
	d.Abs(d)
	return d.Cmp(big.NewInt(1)) <= 0
}

// ---------- Balancer fixed-point helpers (18 decimals) ----------

func fpMulDown(a, b *big.Int) *big.Int {
	return new(big.Int).Quo(new(big.Int).Mul(a, b), fpOne)
}

func fpMulUp(a, b *big.Int) *big.Int {
	prod := new(big.Int).Mul(a, b)
	if prod.Sign() == 0 {
		return new(big.Int)
	}
	// (prod - 1) / ONE + 1
	r := new(big.Int).Sub(prod, big.NewInt(1))
	r.Quo(r, fpOne)
	return r.Add(r, big.NewInt(1))
}

func fpDivDown(a, scalingFactor *big.Int) *big.Int {
	if scalingFactor.Sign() <= 0 {
		return new(big.Int)
	}
	return new(big.Int).Quo(new(big.Int).Mul(a, fpOne), scalingFactor)
}

func divUpInt(a, b *big.Int) *big.Int {
	if b.Sign() <= 0 {
		return new(big.Int)
	}
	q, r := new(big.Int).QuoRem(a, b, new(big.Int))
	if r.Sign() != 0 {
		q.Add(q, big.NewInt(1))
	}
	return q
}

// feeBfp returns the pool fee as an 18-decimal fixed point value.
func (p *Pool) feeBfp() *big.Int {
	if p.FeeDen == nil || p.FeeDen.Sign() == 0 {
		return new(big.Int)
	}
	v := new(big.Int).Mul(p.FeeNum, fpOne)
	return v.Quo(v, p.FeeDen)
}
