package amm

import (
	"errors"
	"math/big"
)

var (
	errU256Overflow  = errors.New("uint256 overflow")
	errU256Underflow = errors.New("uint256 underflow")
)

func isU256(value *big.Int) bool {
	return value != nil && value.Sign() >= 0 && value.Cmp(maxU256) <= 0
}

func addU256(a, b *big.Int) (*big.Int, error) {
	if !isU256(a) || !isU256(b) {
		return nil, errU256Overflow
	}
	result := new(big.Int).Add(a, b)
	if !isU256(result) {
		return nil, errU256Overflow
	}
	return result, nil
}

func subU256(a, b *big.Int) (*big.Int, error) {
	if !isU256(a) || !isU256(b) {
		return nil, errU256Overflow
	}
	if a.Cmp(b) < 0 {
		return nil, errU256Underflow
	}
	return new(big.Int).Sub(a, b), nil
}

func mulU256(a, b *big.Int) (*big.Int, error) {
	if !isU256(a) || !isU256(b) {
		return nil, errU256Overflow
	}
	result := new(big.Int).Mul(a, b)
	if !isU256(result) {
		return nil, errU256Overflow
	}
	return result, nil
}

func divDownU256(a, b *big.Int) (*big.Int, error) {
	if !isU256(a) || !isU256(b) {
		return nil, errU256Overflow
	}
	if b.Sign() == 0 {
		return nil, errors.New("division by zero")
	}
	return new(big.Int).Quo(a, b), nil
}

func divUpU256(a, b *big.Int) (*big.Int, error) {
	if !isU256(a) || !isU256(b) {
		return nil, errU256Overflow
	}
	if b.Sign() == 0 {
		return nil, errors.New("division by zero")
	}
	quotient, remainder := new(big.Int).QuoRem(a, b, new(big.Int))
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !isU256(quotient) {
		return nil, errU256Overflow
	}
	return quotient, nil
}
