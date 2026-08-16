package amm

import (
	"math/big"
	"testing"
)

func TestConcentratedRejectsUnconsumedExactInput(t *testing.T) {
	pool := concentratedPool()
	pool.Ticks = nil
	amount := new(big.Int).Exp(big.NewInt(10), big.NewInt(45), nil)
	if _, err := pool.QuoteExactIn("0x0a", amount); err == nil {
		t.Fatal("quote accepted an exact input that could not be fully consumed")
	}
}

func TestConcentratedFeeMustMapExactlyToPips(t *testing.T) {
	pool := concentratedPool()
	pool.FeeNum = big.NewInt(1)
	pool.FeeDen = big.NewInt(3)
	if _, err := pool.QuoteExactIn("0x0a", big.NewInt(1_000_000)); err == nil {
		t.Fatal("quote accepted a fee that is not exactly representable in pips")
	}
}
