package amm

import (
	"errors"
	"math/big"
)

// Balancer StableMath, ported to exact integer arithmetic with checked uint256
// operations matching the pinned upstream implementation.
var (
	ampPrecision = big.NewInt(1000)
	fpOne        = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

	ErrStableNoConverge = errors.New("stable math did not converge")
)

const stableMaxIterations = 255

// quoteStable returns the output amount for selling amountIn of tokenIn.
func (p *Pool) quoteStable(tokenIn string, amountIn *big.Int) (*big.Int, error) {
	inIdx, outIdx := -1, -1
	for i, token := range p.TokenList {
		if token == tokenIn {
			inIdx = i
		}
	}
	if inIdx < 0 {
		return nil, errors.New("token not in pool")
	}
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
	if !isU256(amountIn) || amountIn.Sign() <= 0 ||
		!isU256(p.AmplificationRaw) || p.AmplificationRaw.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}

	feeBfp, err := p.feeBfp()
	if err != nil || feeBfp.Cmp(fpOne) >= 0 {
		return nil, errors.New("invalid pool fee")
	}
	fee, err := fpMulUp(amountIn, feeBfp)
	if err != nil {
		return nil, err
	}
	amountAfterFee, err := subU256(amountIn, fee)
	if err != nil || amountAfterFee.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}

	balances := make([]*big.Int, n)
	for i := range balances {
		if !isU256(p.Balances[i]) || p.Balances[i].Sign() <= 0 ||
			!isU256(p.ScalingFactors[i]) || p.ScalingFactors[i].Sign() <= 0 {
			return nil, ErrNoLiquidity
		}
		balances[i], err = fpMulDown(p.Balances[i], p.ScalingFactors[i])
		if err != nil || balances[i].Sign() <= 0 {
			return nil, ErrNoLiquidity
		}
	}
	upIn, err := fpMulDown(amountAfterFee, p.ScalingFactors[inIdx])
	if err != nil || upIn.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}

	invariant, err := stableInvariant(p.AmplificationRaw, balances)
	if err != nil || invariant.Sign() <= 0 {
		return nil, err
	}

	balances[inIdx], err = addU256(balances[inIdx], upIn)
	if err != nil {
		return nil, err
	}
	finalOut, err := stableBalanceGivenInvariant(p.AmplificationRaw, balances, invariant, outIdx)
	if err != nil {
		return nil, err
	}
	balances[inIdx], err = subU256(balances[inIdx], upIn)
	if err != nil {
		return nil, err
	}

	upOut, err := subU256(balances[outIdx], finalOut)
	if err != nil {
		return nil, err
	}
	upOut, err = subU256(upOut, big.NewInt(1))
	if err != nil || upOut.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}

	out, err := fpDivDown(upOut, p.ScalingFactors[outIdx])
	if err != nil || out.Sign() <= 0 || out.Cmp(p.Balances[outIdx]) >= 0 {
		return nil, ErrNoLiquidity
	}
	return out, nil
}

// stableInvariant solves for D by Newton iteration.
func stableInvariant(ampTimesPrecision *big.Int, balances []*big.Int) (*big.Int, error) {
	if len(balances) == 0 || !isU256(ampTimesPrecision) {
		return nil, ErrNoLiquidity
	}
	nBig := big.NewInt(int64(len(balances)))

	sum := new(big.Int)
	var err error
	for _, balance := range balances {
		if !isU256(balance) {
			return nil, ErrNoLiquidity
		}
		sum, err = addU256(sum, balance)
		if err != nil {
			return nil, err
		}
	}
	if sum.Sign() == 0 {
		return new(big.Int), nil
	}

	invariant := new(big.Int).Set(sum)
	ampTimesTotal, err := mulU256(ampTimesPrecision, nBig)
	if err != nil {
		return nil, err
	}

	for i := 0; i < stableMaxIterations; i++ {
		dP := new(big.Int).Set(invariant)
		for _, balance := range balances {
			product, err := mulU256(dP, invariant)
			if err != nil {
				return nil, err
			}
			denominator, err := mulU256(balance, nBig)
			if err != nil || denominator.Sign() == 0 {
				return nil, ErrNoLiquidity
			}
			dP, err = divDownU256(product, denominator)
			if err != nil {
				return nil, err
			}
		}
		previous := new(big.Int).Set(invariant)

		term, err := mulU256(ampTimesTotal, sum)
		if err != nil {
			return nil, err
		}
		term, err = divDownU256(term, ampPrecision)
		if err != nil {
			return nil, err
		}
		dPTimesN, err := mulU256(dP, nBig)
		if err != nil {
			return nil, err
		}
		numerator, err := addU256(term, dPTimesN)
		if err != nil {
			return nil, err
		}
		numerator, err = mulU256(numerator, invariant)
		if err != nil {
			return nil, err
		}

		ampMinusPrecision, err := subU256(ampTimesTotal, ampPrecision)
		if err != nil {
			return nil, err
		}
		denominator, err := mulU256(ampMinusPrecision, invariant)
		if err != nil {
			return nil, err
		}
		denominator, err = divDownU256(denominator, ampPrecision)
		if err != nil {
			return nil, err
		}
		nPlusOne, err := addU256(nBig, big.NewInt(1))
		if err != nil {
			return nil, err
		}
		lastTerm, err := mulU256(nPlusOne, dP)
		if err != nil {
			return nil, err
		}
		denominator, err = addU256(denominator, lastTerm)
		if err != nil || denominator.Sign() <= 0 {
			return nil, ErrNoLiquidity
		}

		invariant, err = divDownU256(numerator, denominator)
		if err != nil {
			return nil, err
		}
		if converged(invariant, previous) {
			return invariant, nil
		}
	}
	return nil, ErrStableNoConverge
}

// stableBalanceGivenInvariant solves for one token's balance holding D fixed.
func stableBalanceGivenInvariant(ampTimesPrecision *big.Int, balances []*big.Int, invariant *big.Int, tokenIndex int) (*big.Int, error) {
	if len(balances) == 0 || tokenIndex < 0 || tokenIndex >= len(balances) ||
		!isU256(ampTimesPrecision) || !isU256(invariant) {
		return nil, ErrNoLiquidity
	}
	nBig := big.NewInt(int64(len(balances)))
	ampTimesTotal, err := mulU256(ampTimesPrecision, nBig)
	if err != nil {
		return nil, err
	}

	sum := new(big.Int).Set(balances[0])
	pD, err := mulU256(sum, nBig)
	if err != nil {
		return nil, err
	}
	for _, balance := range balances[1:] {
		pD, err = mulU256(pD, balance)
		if err != nil {
			return nil, err
		}
		pD, err = mulU256(pD, nBig)
		if err != nil {
			return nil, err
		}
		pD, err = divDownU256(pD, invariant)
		if err != nil {
			return nil, err
		}
		sum, err = addU256(sum, balance)
		if err != nil {
			return nil, err
		}
	}
	sum, err = subU256(sum, balances[tokenIndex])
	if err != nil {
		return nil, err
	}

	inv2, err := mulU256(invariant, invariant)
	if err != nil {
		return nil, err
	}
	denominator, err := mulU256(ampTimesTotal, pD)
	if err != nil || denominator.Sign() <= 0 {
		return nil, ErrNoLiquidity
	}
	c, err := divUpU256(inv2, denominator)
	if err != nil {
		return nil, err
	}
	c, err = mulU256(c, ampPrecision)
	if err != nil {
		return nil, err
	}
	c, err = mulU256(c, balances[tokenIndex])
	if err != nil {
		return nil, err
	}

	b, err := divDownU256(invariant, ampTimesTotal)
	if err != nil {
		return nil, err
	}
	b, err = mulU256(b, ampPrecision)
	if err != nil {
		return nil, err
	}
	b, err = addU256(sum, b)
	if err != nil {
		return nil, err
	}

	top, err := addU256(inv2, c)
	if err != nil {
		return nil, err
	}
	bottom, err := addU256(invariant, b)
	if err != nil {
		return nil, err
	}
	tokenBalance, err := divUpU256(top, bottom)
	if err != nil {
		return nil, err
	}
	for i := 0; i < stableMaxIterations; i++ {
		previous := new(big.Int).Set(tokenBalance)
		numerator, err := mulU256(tokenBalance, tokenBalance)
		if err != nil {
			return nil, err
		}
		numerator, err = addU256(numerator, c)
		if err != nil {
			return nil, err
		}
		denominator, err := mulU256(tokenBalance, big.NewInt(2))
		if err != nil {
			return nil, err
		}
		denominator, err = addU256(denominator, b)
		if err != nil {
			return nil, err
		}
		denominator, err = subU256(denominator, invariant)
		if err != nil || denominator.Sign() <= 0 {
			return nil, ErrNoLiquidity
		}
		tokenBalance, err = divUpU256(numerator, denominator)
		if err != nil {
			return nil, err
		}
		if converged(tokenBalance, previous) {
			return tokenBalance, nil
		}
	}
	return nil, ErrStableNoConverge
}

func converged(current, previous *big.Int) bool {
	difference := new(big.Int).Sub(current, previous)
	difference.Abs(difference)
	return difference.Cmp(big.NewInt(1)) <= 0
}

// ---------- Balancer fixed-point helpers (18 decimals) ----------

func fpMulDown(a, b *big.Int) (*big.Int, error) {
	product, err := mulU256(a, b)
	if err != nil {
		return nil, err
	}
	return divDownU256(product, fpOne)
}

func fpMulUp(a, b *big.Int) (*big.Int, error) {
	product, err := mulU256(a, b)
	if err != nil {
		return nil, err
	}
	if product.Sign() == 0 {
		return new(big.Int), nil
	}
	product, err = subU256(product, big.NewInt(1))
	if err != nil {
		return nil, err
	}
	result, err := divDownU256(product, fpOne)
	if err != nil {
		return nil, err
	}
	return addU256(result, big.NewInt(1))
}

func fpDivDown(a, scalingFactor *big.Int) (*big.Int, error) {
	if scalingFactor == nil || scalingFactor.Sign() <= 0 {
		return nil, errors.New("division by zero")
	}
	inflated, err := mulU256(a, fpOne)
	if err != nil {
		return nil, err
	}
	return divDownU256(inflated, scalingFactor)
}

// feeBfp returns the pool fee as an 18-decimal fixed point value.
func (p *Pool) feeBfp() (*big.Int, error) {
	if !isU256(p.FeeNum) || !isU256(p.FeeDen) || p.FeeDen.Sign() == 0 {
		return nil, errors.New("invalid pool fee")
	}
	scaled, err := mulU256(p.FeeNum, fpOne)
	if err != nil {
		return nil, err
	}
	return divDownU256(scaled, p.FeeDen)
}
