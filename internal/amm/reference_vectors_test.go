package amm

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

type referenceVectors struct {
	Schema          string `json:"schema"`
	ConstantProduct []struct {
		ReserveIn  string `json:"reserveIn"`
		ReserveOut string `json:"reserveOut"`
		AmountIn   string `json:"amountIn"`
		AmountOut  string `json:"amountOut"`
		FeeNum     string `json:"feeNum"`
		FeeDen     string `json:"feeDen"`
	} `json:"constantProduct"`
	TickMath []struct {
		Tick         int32  `json:"tick"`
		SqrtPriceX96 string `json:"sqrtPriceX96"`
	} `json:"tickMath"`
	Concentrated []struct {
		SqrtPriceX96 string `json:"sqrtPriceX96"`
		Liquidity    string `json:"liquidity"`
		AmountIn     string `json:"amountIn"`
		AmountOut    string `json:"amountOut"`
		FeePips      int64  `json:"feePips"`
		ZeroForOne   bool   `json:"zeroForOne"`
	} `json:"concentratedLiquidity"`
	Stable []struct {
		Balances         []string `json:"balances"`
		ScalingFactors   []string `json:"scalingFactors"`
		AmplificationRaw string   `json:"amplificationRaw"`
		FeeNum           string   `json:"feeNum"`
		FeeDen           string   `json:"feeDen"`
		TokenIn          int      `json:"tokenIn"`
		TokenOut         int      `json:"tokenOut"`
		AmountIn         string   `json:"amountIn"`
		AmountOut        string   `json:"amountOut"`
	} `json:"stable"`
}

func loadReferenceVectors(t *testing.T) referenceVectors {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "reference", "pool-vectors-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vectors referenceVectors
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if vectors.Schema != "aladdin-pool-reference-v1" {
		t.Fatalf("unexpected vector schema %q", vectors.Schema)
	}
	return vectors
}

func TestIndependentConstantProductVectors(t *testing.T) {
	for _, vector := range loadReferenceVectors(t).ConstantProduct {
		pool := &Pool{
			Kind: "constantProduct", TokenA: "0xa", TokenB: "0xb",
			ReserveA: bi(vector.ReserveIn), ReserveB: bi(vector.ReserveOut),
			FeeNum: bi(vector.FeeNum), FeeDen: bi(vector.FeeDen),
		}
		out, err := pool.QuoteExactInPair("0xa", "0xb", bi(vector.AmountIn))
		if err != nil {
			t.Fatal(err)
		}
		if out.String() != vector.AmountOut {
			t.Fatalf("got %s want %s", out, vector.AmountOut)
		}
	}
}

func TestIndependentTickMathVectors(t *testing.T) {
	for _, vector := range loadReferenceVectors(t).TickMath {
		value, err := GetSqrtRatioAtTick(vector.Tick)
		if err != nil {
			t.Fatal(err)
		}
		if value.String() != vector.SqrtPriceX96 {
			t.Fatalf("tick %d: got %s want %s", vector.Tick, value, vector.SqrtPriceX96)
		}
	}
}

func TestIndependentConcentratedVector(t *testing.T) {
	for _, vector := range loadReferenceVectors(t).Concentrated {
		feeNum := big.NewInt(vector.FeePips)
		pool := &Pool{
			Kind: "concentratedLiquidity", TokenA: "0xa", TokenB: "0xb",
			SqrtPriceX96: bi(vector.SqrtPriceX96), Liquidity: bi(vector.Liquidity), Tick: 0,
			FeeNum: feeNum, FeeDen: big.NewInt(1_000_000),
		}
		tokenIn, tokenOut := "0xa", "0xb"
		if !vector.ZeroForOne {
			tokenIn, tokenOut = tokenOut, tokenIn
		}
		out, err := pool.QuoteExactInPair(tokenIn, tokenOut, bi(vector.AmountIn))
		if err != nil {
			t.Fatal(err)
		}
		if out.String() != vector.AmountOut {
			t.Fatalf("got %s want %s", out, vector.AmountOut)
		}
	}
}

func TestIndependentStableVectors(t *testing.T) {
	for _, vector := range loadReferenceVectors(t).Stable {
		balances := make([]*big.Int, len(vector.Balances))
		scales := make([]*big.Int, len(vector.ScalingFactors))
		tokens := make([]string, len(vector.Balances))
		for i := range balances {
			balances[i] = bi(vector.Balances[i])
			scales[i] = bi(vector.ScalingFactors[i])
			tokens[i] = string(rune('a' + i))
		}
		pool := &Pool{
			Kind: "stable", TokenList: tokens, Balances: balances, ScalingFactors: scales,
			AmplificationRaw: bi(vector.AmplificationRaw), FeeNum: bi(vector.FeeNum), FeeDen: bi(vector.FeeDen),
		}
		out, err := pool.quoteStableIndexed(vector.TokenIn, vector.TokenOut, bi(vector.AmountIn))
		if err != nil {
			t.Fatal(err)
		}
		if out.String() != vector.AmountOut {
			t.Fatalf("got %s want %s", out, vector.AmountOut)
		}
	}
}
