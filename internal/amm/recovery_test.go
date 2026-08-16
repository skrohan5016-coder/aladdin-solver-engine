package amm

import (
	"math/big"
	"strings"
	"testing"
)

func TestDecimalParserRejectsUnboundedInput(t *testing.T) {
	for _, invalid := range []string{"+1", "1.2.3", strings.Repeat("1", 129)} {
		if _, _, err := ParseDecimalRational(invalid); err == nil {
			t.Errorf("expected %q to be rejected", invalid)
		}
	}
}

func TestConstantProductRejectsCheckedOverflow(t *testing.T) {
	pool := &Pool{
		Kind: "constantProduct", TokenA: "0xa", TokenB: "0xb",
		ReserveA: bi("1000000"), ReserveB: bi("1000000"),
		FeeNum: big.NewInt(0), FeeDen: big.NewInt(1000),
	}
	if _, err := pool.QuoteExactIn("0xa", new(big.Int).Set(maxU256)); err == nil {
		t.Fatal("expected uint256 multiplication overflow to be rejected")
	}
	tooLarge := new(big.Int).Lsh(big.NewInt(1), 256)
	if _, err := pool.QuoteExactIn("0xa", tooLarge); err == nil {
		t.Fatal("expected amount above uint256 to be rejected")
	}
}

func TestStableMatchesPinnedReferenceVector(t *testing.T) {
	pool := &Pool{
		Kind:             "stable",
		TokenA:           "0xa",
		TokenB:           "0xb",
		TokenList:        []string{"0xa", "0xb"},
		Balances:         []*big.Int{bi("10000000000000000000"), bi("12000000000000000000")},
		ScalingFactors:   []*big.Int{new(big.Int).Set(fpOne), new(big.Int).Set(fpOne)},
		AmplificationRaw: big.NewInt(100_000),
		FeeNum:           big.NewInt(0),
		FeeDen:           big.NewInt(1),
	}
	output, err := pool.QuoteExactInPair("0xa", "0xb", bi("1000000000000000000"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "1000907478822554418"
	if output.String() != want {
		t.Fatalf("stable reference output = %s, want %s", output, want)
	}
}

func TestStableRejectsCheckedOverflow(t *testing.T) {
	pool := &Pool{
		Kind:             "stable",
		TokenA:           "0xa",
		TokenB:           "0xb",
		TokenList:        []string{"0xa", "0xb"},
		Balances:         []*big.Int{new(big.Int).Set(maxU256), new(big.Int).Set(maxU256)},
		ScalingFactors:   []*big.Int{new(big.Int).Set(fpOne), new(big.Int).Set(fpOne)},
		AmplificationRaw: big.NewInt(100_000),
		FeeNum:           big.NewInt(0),
		FeeDen:           big.NewInt(1),
	}
	if _, err := pool.QuoteExactInPair("0xa", "0xb", big.NewInt(1)); err == nil {
		t.Fatal("expected checked stable arithmetic overflow")
	}
}
