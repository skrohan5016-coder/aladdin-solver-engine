package amm

import (
	"math/big"
	"testing"
)

func bi(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("bad int " + s)
	}
	return v
}

func TestParseDecimalRational(t *testing.T) {
	cases := []struct{ in, num, den string }{
		{"0.003", "3", "1000"},
		{"0.0005", "5", "10000"},
		{"0", "0", "1"},
		{"1", "1", "1"},
		{"0.30", "30", "100"},
		{".003", "3", "1000"},
	}
	for _, c := range cases {
		n, d, err := ParseDecimalRational(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if n.String() != c.num || d.String() != c.den {
			t.Errorf("%s: got %s/%s want %s/%s", c.in, n, d, c.num, c.den)
		}
	}
	if _, _, err := ParseDecimalRational("1e-3"); err == nil {
		t.Error("expected exponent notation to be rejected")
	}
}

// Reference values computed from the UniswapV2 getAmountOut formula.
func TestConstantProductMatchesUniswapV2(t *testing.T) {
	mk := func(ra, rb string) *Pool {
		return &Pool{
			Kind: "constantProduct", TokenA: "0xa", TokenB: "0xb",
			ReserveA: bi(ra), ReserveB: bi(rb),
			FeeNum: big.NewInt(3), FeeDen: big.NewInt(1000),
		}
	}
	cases := []struct{ ra, rb, in, want string }{
		{"1000000", "1000000", "1000", "996"},
		{"100000000000000000000", "200000000000000000000000", "1000000000000000000", "1974316068794122597700"},
	}
	for _, c := range cases {
		got, err := mk(c.ra, c.rb).QuoteExactIn("0xa", bi(c.in))
		if err != nil {
			t.Fatalf("quote: %v", err)
		}
		if got.String() != c.want {
			t.Errorf("in=%s got %s want %s", c.in, got, c.want)
		}
	}
}

func TestConstantProductRejectsDustAndBadFee(t *testing.T) {
	p := &Pool{
		Kind: "constantProduct", TokenA: "0xa", TokenB: "0xb",
		ReserveA: bi("1000000"), ReserveB: bi("1000000"),
		FeeNum: big.NewInt(3), FeeDen: big.NewInt(1000),
	}
	if _, err := p.QuoteExactIn("0xa", big.NewInt(1)); err == nil {
		t.Error("expected error for an input that rounds to zero output")
	}
	p.FeeNum = big.NewInt(-1)
	if _, err := p.QuoteExactIn("0xa", big.NewInt(100)); err == nil {
		t.Error("expected a negative fee to be rejected")
	}
}

func TestConstantProductMonotonic(t *testing.T) {
	p := &Pool{
		Kind: "constantProduct", TokenA: "0xa", TokenB: "0xb",
		ReserveA: bi("5000000000000000000000"), ReserveB: bi("9000000000000000000000"),
		FeeNum: big.NewInt(3), FeeDen: big.NewInt(1000),
	}
	prev := big.NewInt(0)
	for i := 1; i <= 40; i++ {
		in := new(big.Int).Mul(big.NewInt(int64(i)), bi("1000000000000000000"))
		out, err := p.QuoteExactIn("0xa", in)
		if err != nil {
			t.Fatal(err)
		}
		if out.Cmp(prev) <= 0 {
			t.Fatalf("output not monotonic at i=%d: %s <= %s", i, out, prev)
		}
		prev = out
	}
}

// Reference values from UniswapV3 TickMath.
func TestGetSqrtRatioAtTick(t *testing.T) {
	cases := []struct {
		tick int32
		want string
	}{
		{0, "79228162514264337593543950336"},
		{1, "79232123823359799118286999568"},
		{-1, "79224201403219477170569942574"},
		{MinTick, "4295128739"},
		{MaxTick, "1461446703485210103287273052203988822378723970342"},
	}
	for _, c := range cases {
		got, err := GetSqrtRatioAtTick(c.tick)
		if err != nil {
			t.Fatalf("tick %d: %v", c.tick, err)
		}
		if got.String() != c.want {
			t.Errorf("tick %d: got %s want %s", c.tick, got, c.want)
		}
	}
	if _, err := GetSqrtRatioAtTick(MaxTick + 1); err == nil {
		t.Error("expected out-of-range tick to error")
	}
}

func TestGetSqrtRatioMonotonic(t *testing.T) {
	prev, _ := GetSqrtRatioAtTick(-5000)
	for tick := int32(-4999); tick <= 5000; tick++ {
		cur, err := GetSqrtRatioAtTick(tick)
		if err != nil {
			t.Fatal(err)
		}
		if cur.Cmp(prev) <= 0 {
			t.Fatalf("sqrt ratio not increasing at tick %d", tick)
		}
		prev = cur
	}
}

// concentratedPool builds a single-range V3 pool centred at tick 0.
func concentratedPool() *Pool {
	return &Pool{
		Kind: "concentratedLiquidity", TokenA: "0x0a", TokenB: "0x0b",
		SqrtPriceX96: bi("79228162514264337593543950336"),
		Liquidity:    bi("100000000000000000000"),
		Tick:         0,
		Ticks: []Tick{
			{Index: -60000, Net: bi("100000000000000000000")},
			{Index: 60000, Net: bi("-100000000000000000000")},
		},
		FeeNum: big.NewInt(3), FeeDen: big.NewInt(1000),
	}
}

func TestConcentratedQuoteSane(t *testing.T) {
	p := concentratedPool()
	in := bi("1000000000000000000") // 1e18
	out, err := p.QuoteExactIn("0x0a", in)
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if out.Cmp(in) >= 0 {
		t.Errorf("output %s should be below input %s at 1:1 price", out, in)
	}
	floor := new(big.Int).Quo(in, big.NewInt(2))
	if out.Cmp(floor) <= 0 {
		t.Errorf("output %s implausibly low for a deep pool", out)
	}
}

func TestConcentratedMonotonicAndSymmetric(t *testing.T) {
	prev := big.NewInt(0)
	for i := 1; i <= 20; i++ {
		p := concentratedPool()
		in := new(big.Int).Mul(big.NewInt(int64(i)), bi("100000000000000000"))
		out, err := p.QuoteExactIn("0x0a", in)
		if err != nil {
			t.Fatalf("i=%d: %v", i, err)
		}
		if out.Cmp(prev) <= 0 {
			t.Fatalf("not monotonic at i=%d", i)
		}
		prev = out
	}
	a, err := concentratedPool().QuoteExactIn("0x0a", bi("1000000000000000000"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := concentratedPool().QuoteExactIn("0x0b", bi("1000000000000000000"))
	if err != nil {
		t.Fatal(err)
	}
	diff := new(big.Int).Sub(a, b)
	diff.Abs(diff)
	tol := new(big.Int).Quo(a, big.NewInt(1000))
	if diff.Cmp(tol) > 0 {
		t.Errorf("directional asymmetry too large: %s vs %s", a, b)
	}
}

func stablePool(tokens ...string) *Pool {
	balances := make([]*big.Int, len(tokens))
	scales := make([]*big.Int, len(tokens))
	for i := range tokens {
		balances[i] = bi("1000000000000000000000000")
		scales[i] = bi("1000000000000000000")
	}
	return &Pool{
		Kind:             "stable",
		TokenA:           tokens[0],
		TokenB:           tokens[1],
		TokenList:        tokens,
		Balances:         balances,
		ScalingFactors:   scales,
		AmplificationRaw: big.NewInt(100_000),
		FeeNum:           big.NewInt(4),
		FeeDen:           big.NewInt(10_000),
	}
}

func TestStableQuoteSaneAndDeterministic(t *testing.T) {
	p := stablePool("0xa", "0xb")
	in := bi("1000000000000000000")
	first, err := p.QuoteExactIn("0xa", in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.QuoteExactInPair("0xa", "0xb", in)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cmp(second) != 0 {
		t.Fatalf("same quote was not deterministic: %s != %s", first, second)
	}
	if first.Cmp(in) >= 0 {
		t.Fatalf("stable quote %s should include fee and be below input %s", first, in)
	}
	floor := new(big.Int).Mul(in, big.NewInt(99))
	floor.Quo(floor, big.NewInt(100))
	if first.Cmp(floor) <= 0 {
		t.Fatalf("stable quote %s is implausibly low", first)
	}
}

func TestStablePoolSupportsEveryExplicitPair(t *testing.T) {
	p := stablePool("0xa", "0xb", "0xc")
	if !p.Supports("0xa", "0xc") || !p.Supports("0xc", "0xb") {
		t.Fatal("multi-token stable pool did not expose all token pairs")
	}
	if _, err := p.QuoteExactIn("0xa", bi("1000000000000000000")); err == nil {
		t.Fatal("ambiguous multi-token quote should require an output token")
	}
	out, err := p.QuoteExactInPair("0xa", "0xc", bi("1000000000000000000"))
	if err != nil || out.Sign() <= 0 {
		t.Fatalf("explicit three-token quote failed: out=%v err=%v", out, err)
	}
}

func TestUnsupportedKindRejected(t *testing.T) {
	p := &Pool{Kind: "weightedProduct", TokenA: "0xa", TokenB: "0xb", FeeNum: big.NewInt(1), FeeDen: big.NewInt(1000)}
	if _, err := p.QuoteExactIn("0xa", big.NewInt(100)); err != ErrUnsupportedKind {
		t.Errorf("got %v want ErrUnsupportedKind", err)
	}
}
