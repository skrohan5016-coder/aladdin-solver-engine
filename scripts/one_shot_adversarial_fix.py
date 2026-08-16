#!/usr/bin/env python3
from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one replacement, found {count}")
    file.write_text(text.replace(old, new, 1))


# Reject V3 fees that cannot be represented exactly in Uniswap pips.
replace_once(
    "internal/amm/amm.go",
    '''\tpips := new(big.Int).Mul(p.FeeNum, big.NewInt(1_000_000))
\tpips.Quo(pips, p.FeeDen)
\tif !isU256(pips) || pips.Cmp(big.NewInt(1_000_000)) >= 0 {
''',
    '''\tscaled := new(big.Int).Mul(p.FeeNum, big.NewInt(1_000_000))
\tpips, remainder := new(big.Int).QuoRem(scaled, p.FeeDen, new(big.Int))
\tif remainder.Sign() != 0 || !isU256(pips) || pips.Cmp(big.NewInt(1_000_000)) >= 0 {
''',
)

# Fail closed when the pool is already at the direction's terminal price.
replace_once(
    "internal/amm/amm.go",
    '''\tlimit := new(big.Int).Sub(maxSqrtRatio, big.NewInt(1))
\tif zeroForOne {
\t\tlimit = new(big.Int).Add(minSqrtRatio, big.NewInt(1))
\t}

\tidx := sort.Search(len(p.Ticks), func(i int) bool { return p.Ticks[i].Index > p.Tick })
''',
    '''\tlimit := new(big.Int).Sub(maxSqrtRatio, big.NewInt(1))
\tif zeroForOne {
\t\tlimit = new(big.Int).Add(minSqrtRatio, big.NewInt(1))
\t}
\tif (zeroForOne && sqrtPrice.Cmp(limit) <= 0) || (!zeroForOne && sqrtPrice.Cmp(limit) >= 0) {
\t\treturn nil, ErrNoLiquidity
\t}

\tidx := sort.Search(len(p.Ticks), func(i int) bool { return p.Ticks[i].Index > p.Tick })
''',
)

# A liquidity interaction declares an exact input. Never return a partial V3
# quote after reaching the tick/price/iteration boundary with input remaining.
replace_once(
    "internal/amm/amm.go",
    '''\tif amountOut.Sign() <= 0 {
\t\treturn nil, ErrNoLiquidity
\t}
\treturn amountOut, nil
}

// computeSwapStep mirrors Uniswap V3 SwapMath.computeSwapStep for exact input.
''',
    '''\tif remaining.Sign() != 0 {
\t\treturn nil, errors.New("concentrated quote did not consume the full input")
\t}
\tif amountOut.Sign() <= 0 {
\t\treturn nil, ErrNoLiquidity
\t}
\treturn amountOut, nil
}

// computeSwapStep mirrors Uniswap V3 SwapMath.computeSwapStep for exact input.
''',
)

# Make route gas arithmetic checked and observe cancellation after long quotes.
replace_once(
    "internal/solve/route.go",
    '''\t\tout, err := pool.QuoteExactInPair(sellToken, buyToken, amountIn)
\t\tif err != nil {
\t\t\tcontinue
\t\t}
\t\tbest = better(best, &Route{
''',
    '''\t\tout, err := pool.QuoteExactInPair(sellToken, buyToken, amountIn)
\t\tif err != nil {
\t\t\tcontinue
\t\t}
\t\tif err := ctx.Err(); err != nil {
\t\t\treturn nil, err
\t\t}
\t\tbest = better(best, &Route{
''',
)
replace_once(
    "internal/solve/route.go",
    '''\t\t\t\tout, err := second.QuoteExactInPair(mid, buyToken, midAmount)
\t\t\t\tif err != nil {
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t\tbest = better(best, &Route{
''',
    '''\t\t\t\tout, err := second.QuoteExactInPair(mid, buyToken, midAmount)
\t\t\t\tif err != nil {
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t\tif err := ctx.Err(); err != nil {
\t\t\t\t\treturn nil, err
\t\t\t\t}
\t\t\t\tgas, ok := sumGas(first.GasEstimate, second.GasEstimate)
\t\t\t\tif !ok {
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t\tbest = better(best, &Route{
''',
)
replace_once(
    "internal/solve/route.go",
    '''\t\t\t\t\tOut: out,
\t\t\t\t\tGas: first.GasEstimate + second.GasEstimate,
\t\t\t\t})
''',
    '''\t\t\t\t\tOut: out,
\t\t\t\t\tGas: gas,
\t\t\t\t})
''',
)
replace_once(
    "internal/solve/route.go",
    '''\t}
\treturn best, nil
}

func (g *Graph) poolsFor(token string) []*amm.Pool {
''',
    '''\t}
\tif err := ctx.Err(); err != nil {
\t\treturn nil, err
\t}
\treturn best, nil
}

func (g *Graph) poolsFor(token string) []*amm.Pool {
''',
)
replace_once(
    "internal/solve/route.go",
    '''func routeKey(route *Route) string {
\tvar builder strings.Builder
\tfor _, hop := range route.Hops {
\t\tbuilder.WriteString(hopKey(&hop))
\t\tbuilder.WriteByte('\\x00')
\t}
\treturn builder.String()
}
''',
    '''func routeKey(route *Route) string {
\tvar builder strings.Builder
\tfor _, hop := range route.Hops {
\t\tbuilder.WriteString(hopKey(&hop))
\t\tbuilder.WriteByte('\\x00')
\t}
\treturn builder.String()
}

func sumGas(values ...uint64) (uint64, bool) {
\tvar total uint64
\tfor _, value := range values {
\t\tif value > ^uint64(0)-total {
\t\t\treturn 0, false
\t\t}
\t\ttotal += value
\t}
\treturn total, true
}
''',
)

# Normalize zero/negative resource caps and check every settlement gas sum.
replace_once(
    "internal/solve/solve.go",
    '''func Solve(ctx context.Context, auction *api.Auction, cfg Config) Result {
\tresult := Result{}
''',
    '''func Solve(ctx context.Context, auction *api.Auction, cfg Config) Result {
\tdefaults := DefaultConfig()
\tif cfg.MaxOrders <= 0 {
\t\tcfg.MaxOrders = defaults.MaxOrders
\t}
\tif cfg.MaxPools <= 0 {
\t\tcfg.MaxPools = defaults.MaxPools
\t}

\tresult := Result{}
''',
)
replace_once(
    "internal/solve/solve.go",
    '''\tgraph := NewGraph(pools)

\torders, unsupported := eligible(auction.Orders, cfg.MaxOrders)
''',
    '''\tgraph := NewGraph(pools)
\tif ctx.Err() != nil {
\t\treturn result
\t}

\torders, unsupported := eligible(auction.Orders, cfg.MaxOrders)
''',
)
replace_once(
    "internal/solve/solve.go",
    '''\t\tgas := cfg.SettlementOverheadGas + 2*cfg.PerTradeGas
\t\tif cfg.RequireProfitable && !cowProfitable(match, gas, gasPrice, auction.Tokens) {
''',
    '''\t\tgas, ok := sumGas(cfg.SettlementOverheadGas, cfg.PerTradeGas, cfg.PerTradeGas)
\t\tif !ok {
\t\t\tresult.Stats.DroppedNotProfitable += 2
\t\t\tcontinue
\t\t}
\t\tif cfg.RequireProfitable && !cowProfitable(match, gas, gasPrice, auction.Tokens) {
''',
)
replace_once(
    "internal/solve/solve.go",
    '''\t\tsolutions = append(solutions, match.solution(id, cfg))
''',
    '''\t\tsolutions = append(solutions, match.solution(id, gas))
''',
)
replace_once(
    "internal/solve/solve.go",
    '''\t\tgas := cfg.SettlementOverheadGas + cfg.PerTradeGas + route.Gas
\t\tsolution := api.Solution{
''',
    '''\t\tgas, ok := sumGas(cfg.SettlementOverheadGas, cfg.PerTradeGas, route.Gas)
\t\tif !ok {
\t\t\treturn api.Solution{}, nil, "", 0, reasonNoRoute
\t\t}
\t\tsolution := api.Solution{
''',
)
replace_once(
    "internal/solve/solve.go",
    '''\tgas := cfg.SettlementOverheadGas + cfg.PerTradeGas + route.Gas
\tsolution := api.Solution{
''',
    '''\tgas, ok := sumGas(cfg.SettlementOverheadGas, cfg.PerTradeGas, route.Gas)
\tif !ok {
\t\treturn api.Solution{}, nil, "", 0, reasonNoRoute
\t}
\tsolution := api.Solution{
''',
)
replace_once(
    "internal/solve/solve.go",
    '''func (match Match) solution(id uint64, cfg Config) api.Solution {
''',
    '''func (match Match) solution(id, gas uint64) api.Solution {
''',
)
replace_once(
    "internal/solve/solve.go",
    '''\t\tInteractions: []api.Interaction{},
\t\tGas:          cfg.SettlementOverheadGas + 2*cfg.PerTradeGas,
''',
    '''\t\tInteractions: []api.Interaction{},
\t\tGas:          gas,
''',
)

# Reject semantic duplicates, while allowing opaque IDs on liquidity kinds the
# engine does not consume or emit.
replace_once(
    "internal/server/server.go",
    '''\t"strconv"
\t"time"
''',
    '''\t"strconv"
\t"strings"
\t"time"
''',
)
replace_once(
    "internal/server/server.go",
    '''\tfor address, token := range auction.Tokens {
\t\tif address == "" || !validU256(token.AvailableBalance, true) {
\t\t\treturn fmt.Errorf("invalid token %q", address)
\t\t}
\t\tif token.ReferencePrice != "" && !validU256(token.ReferencePrice, true) {
\t\t\treturn fmt.Errorf("invalid reference price for token %q", address)
\t\t}
\t}
\tfor i, order := range auction.Orders {
''',
    '''\tseenTokenAddresses := map[string]struct{}{}
\tfor address, token := range auction.Tokens {
\t\tnormalized := strings.ToLower(address)
\t\tif normalized == "" || !validU256(token.AvailableBalance, true) {
\t\t\treturn fmt.Errorf("invalid token %q", address)
\t\t}
\t\tif _, duplicate := seenTokenAddresses[normalized]; duplicate {
\t\t\treturn fmt.Errorf("duplicate token address %q", address)
\t\t}
\t\tseenTokenAddresses[normalized] = struct{}{}
\t\tif token.ReferencePrice != "" && !validU256(token.ReferencePrice, true) {
\t\t\treturn fmt.Errorf("invalid reference price for token %q", address)
\t\t}
\t}
\tseenOrderUIDs := map[string]struct{}{}
\tfor i, order := range auction.Orders {
''',
)
replace_once(
    "internal/server/server.go",
    '''\t\tif order.FullSellAmount != "" && !validU256(order.FullSellAmount, false) {
\t\t\treturn fmt.Errorf("invalid full sell amount for order %d", i)
\t\t}
\t}
\tfor i, liquidity := range auction.Liquidity {
\t\tif !decimalDigits(liquidity.ID, 20) {
\t\t\treturn fmt.Errorf("invalid liquidity id at index %d", i)
\t\t}
\t\tif _, err := strconv.ParseUint(liquidity.ID, 10, 64); err != nil {
\t\t\treturn fmt.Errorf("invalid liquidity id at index %d: %w", i, err)
\t\t}
\t}
\treturn nil
}

func validU256(raw string, allowZero bool) bool {
''',
    '''\t\tif order.FullSellAmount != "" && !validU256(order.FullSellAmount, false) {
\t\t\treturn fmt.Errorf("invalid full sell amount for order %d", i)
\t\t}
\t\tuid := strings.ToLower(order.UID)
\t\tif _, duplicate := seenOrderUIDs[uid]; duplicate {
\t\t\treturn fmt.Errorf("duplicate order uid %q", order.UID)
\t\t}
\t\tseenOrderUIDs[uid] = struct{}{}
\t}
\tseenLiquidityIDs := map[uint64]struct{}{}
\tfor i, liquidity := range auction.Liquidity {
\t\tif !routableLiquidityKind(liquidity.Kind) {
\t\t\tcontinue
\t\t}
\t\tif !decimalDigits(liquidity.ID, 20) {
\t\t\treturn fmt.Errorf("invalid liquidity id at index %d", i)
\t\t}
\t\tid, err := strconv.ParseUint(liquidity.ID, 10, 64)
\t\tif err != nil {
\t\t\treturn fmt.Errorf("invalid liquidity id at index %d: %w", i, err)
\t\t}
\t\tif strconv.FormatUint(id, 10) != liquidity.ID {
\t\t\treturn fmt.Errorf("non-canonical liquidity id at index %d", i)
\t\t}
\t\tif _, duplicate := seenLiquidityIDs[id]; duplicate {
\t\t\treturn fmt.Errorf("duplicate liquidity id %d", id)
\t\t}
\t\tseenLiquidityIDs[id] = struct{}{}
\t}
\treturn nil
}

func routableLiquidityKind(kind string) bool {
\tswitch kind {
\tcase "constantProduct", "concentratedLiquidity", "stable":
\t\treturn true
\tdefault:
\t\treturn false
\t}
}

func validU256(raw string, allowZero bool) bool {
''',
)

# Runtime safety: loopback-only bind, mandatory recorder, and no uint64->int
# wraparound that can silently remove request caps.
replace_once(
    "cmd/solver/main.go",
    '''import (
\t"context"
\t"errors"
\t"log/slog"
\t"net/http"
''',
    '''import (
\t"context"
\t"errors"
\t"fmt"
\t"log/slog"
\t"net"
\t"net/http"
''',
)
replace_once(
    "cmd/solver/main.go",
    '''\t"strconv"
\t"syscall"
''',
    '''\t"strconv"
\t"strings"
\t"syscall"
''',
)
replace_once(
    "cmd/solver/main.go",
    '''\tcfg.MaxSolutions = int(envUint("MAX_SOLUTIONS", uint64(cfg.MaxSolutions)))
\tcfg.MaxOrders = int(envUint("MAX_ORDERS", uint64(cfg.MaxOrders)))
\tcfg.MaxPools = int(envUint("MAX_POOLS", uint64(cfg.MaxPools)))
''',
    '''\tcfg.MaxSolutions = envPositiveInt("MAX_SOLUTIONS", cfg.MaxSolutions)
\tcfg.MaxOrders = envPositiveInt("MAX_ORDERS", cfg.MaxOrders)
\tcfg.MaxPools = envPositiveInt("MAX_POOLS", cfg.MaxPools)
''',
)
replace_once(
    "cmd/solver/main.go",
    '''\trecorder, err := record.New(env("RECORD_DIR", "./data"), envBool("RECORD_FULL_AUCTIONS", false))
\tif err != nil {
\t\tlog.Error("recorder init failed, continuing without it", "err", err)
\t\trecorder = nil
\t} else {
\t\tdefer func() {
\t\t\tif err := recorder.Close(); err != nil {
\t\t\t\tlog.Error("recorder close", "err", err)
\t\t\t}
\t\t}()
\t}

\thttpServer := &http.Server{
\t\tAddr:              env("LISTEN_ADDR", "127.0.0.1:8000"),
''',
    '''\trecorder, err := record.New(env("RECORD_DIR", "./data"), envBool("RECORD_FULL_AUCTIONS", false))
\tif err != nil {
\t\tlog.Error("recorder init failed", "err", err)
\t\tos.Exit(1)
\t}
\tdefer func() {
\t\tif err := recorder.Close(); err != nil {
\t\t\tlog.Error("recorder close", "err", err)
\t\t}
\t}()

\tlistenAddr := env("LISTEN_ADDR", "127.0.0.1:8000")
\tif err := validateListenAddr(listenAddr); err != nil {
\t\tlog.Error("unsafe listen address", "addr", listenAddr, "err", err)
\t\tos.Exit(1)
\t}

\thttpServer := &http.Server{
\t\tAddr:              listenAddr,
''',
)
replace_once(
    "cmd/solver/main.go",
    '''func envBool(key string, fallback bool) bool {
''',
    '''func envPositiveInt(key string, fallback int) int {
\tvalue := os.Getenv(key)
\tif value == "" {
\t\treturn fallback
\t}
\tnumber, err := strconv.ParseUint(value, 10, 64)
\tmaxInt := uint64(^uint(0) >> 1)
\tif err != nil || number == 0 || number > maxInt {
\t\treturn fallback
\t}
\treturn int(number)
}

func validateListenAddr(address string) error {
\thost, _, err := net.SplitHostPort(address)
\tif err != nil {
\t\treturn fmt.Errorf("parse listen address: %w", err)
\t}
\tif strings.EqualFold(host, "localhost") {
\t\treturn nil
\t}
\tip := net.ParseIP(host)
\tif ip == nil || !ip.IsLoopback() {
\t\treturn fmt.Errorf("host %q is not loopback", host)
\t}
\treturn nil
}

func envBool(key string, fallback bool) bool {
''',
)

Path("internal/amm/adversarial_test.go").write_text(r'''package amm

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
''')

Path("internal/solve/adversarial_test.go").write_text(r'''package solve

import (
	"context"
	"math"
	"math/big"
	"testing"
)

func TestSumGasRejectsOverflow(t *testing.T) {
	if _, ok := sumGas(math.MaxUint64, 1); ok {
		t.Fatal("gas addition overflow was accepted")
	}
}

func TestTwoHopRouteRejectsGasOverflow(t *testing.T) {
	liquidity := []api.Liquidity{
		cpPool("1", tokA, tokC, "1000000000000000000000", "1000000000000000000000"),
		cpPool("2", tokC, tokB, "1000000000000000000000", "1000000000000000000000"),
	}
	liquidity[0].GasEstimate = "18446744073709551615"
	liquidity[1].GasEstimate = "1"
	pools, skipped := BuildPools(liquidity)
	if len(pools) != 2 || len(skipped) != 0 {
		t.Fatalf("unexpected pool parse result: pools=%d skipped=%v", len(pools), skipped)
	}
	route := NewGraph(pools).BestRoute(tokA, tokB, big.NewInt(1_000_000))
	if route != nil {
		t.Fatal("router returned a path whose gas estimate overflowed uint64")
	}
}

func TestSettlementGasOverflowDropsCandidate(t *testing.T) {
	order := sellOrder("0xoverflow", tokA, tokB, "1000000000000000000", "1")
	auction := &api.Auction{
		Orders:            []api.Order{order},
		Liquidity:         []api.Liquidity{cpPool("1", tokA, tokB, "1000000000000000000000", "1000000000000000000000")},
		EffectiveGasPrice: "1",
		Tokens:            map[string]api.TokenInfo{},
	}
	config := DefaultConfig()
	config.RequireProfitable = false
	config.SettlementOverheadGas = math.MaxUint64
	config.PerTradeGas = 1
	result := Solve(context.Background(), auction, config)
	if len(result.Solutions) != 0 {
		t.Fatal("solver returned a candidate with wrapped settlement gas")
	}
}
'''.replace('"testing"\n)', '"testing"\n\n\t"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"\n)'))

Path("internal/server/adversarial_test.go").write_text(r'''package server

import (
	"testing"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
)

func validContractAuction() *api.Auction {
	return &api.Auction{
		Tokens:                        map[string]api.TokenInfo{},
		Orders:                        []api.Order{},
		Liquidity:                     []api.Liquidity{},
		EffectiveGasPrice:             "1",
		SurplusCapturingJitOrderOwner: []string{},
	}
}

func validContractOrder(uid string) api.Order {
	return api.Order{
		UID:               uid,
		SellToken:         "0x0000000000000000000000000000000000000001",
		BuyToken:          "0x0000000000000000000000000000000000000002",
		SellAmount:        "1",
		BuyAmount:         "1",
		FullBuyAmount:     "1",
		Kind:              "sell",
		Owner:             "0x0000000000000000000000000000000000000003",
		SellTokenSource:   "erc20",
		BuyTokenDest:      "erc20",
		PreInteractions:   []json.RawMessage{},
		PostInteractions:  []json.RawMessage{},
		PartiallyFillable: false,
	}
}

func TestOpaqueIDAllowedForUnsupportedLiquidity(t *testing.T) {
	auction := validContractAuction()
	auction.Liquidity = []api.Liquidity{{Kind: "weightedProduct", ID: "opaque-pool-id"}}
	if err := validateAuction(auction); err != nil {
		t.Fatalf("unsupported opaque liquidity should be skipped, got %v", err)
	}
}

func TestRoutableLiquidityIDMustBeCanonicalAndUnique(t *testing.T) {
	for name, liquidity := range map[string][]api.Liquidity{
		"non-canonical": {{Kind: "constantProduct", ID: "01"}},
		"duplicate": {
			{Kind: "constantProduct", ID: "1"},
			{Kind: "stable", ID: "1"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			auction := validContractAuction()
			auction.Liquidity = liquidity
			if err := validateAuction(auction); err == nil {
				t.Fatal("invalid routable liquidity ids were accepted")
			}
		})
	}
}

func TestSemanticDuplicatesRejected(t *testing.T) {
	t.Run("token address", func(t *testing.T) {
		auction := validContractAuction()
		auction.Tokens = map[string]api.TokenInfo{
			"0xAbC": {AvailableBalance: "0"},
			"0xabc": {AvailableBalance: "0"},
		}
		if err := validateAuction(auction); err == nil {
			t.Fatal("case-insensitive duplicate token addresses were accepted")
		}
	})

	t.Run("order uid", func(t *testing.T) {
		auction := validContractAuction()
		auction.Orders = []api.Order{
			validContractOrder("0xAbC"),
			validContractOrder("0xabc"),
		}
		if err := validateAuction(auction); err == nil {
			t.Fatal("case-insensitive duplicate order UIDs were accepted")
		}
	})
}
'''.replace('"testing"\n', '"encoding/json"\n\t"testing"\n'))

Path("cmd/solver/main_test.go").write_text(r'''package main

import "testing"

func TestValidateListenAddr(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1:8000",
		"127.42.0.1:8000",
		"[::1]:8000",
		"localhost:8000",
	} {
		if err := validateListenAddr(address); err != nil {
			t.Errorf("loopback address %q rejected: %v", address, err)
		}
	}
	for _, address := range []string{
		":8000",
		"0.0.0.0:8000",
		"192.0.2.1:8000",
		"example.com:8000",
		"not-an-address",
	} {
		if err := validateListenAddr(address); err == nil {
			t.Errorf("unsafe address %q accepted", address)
		}
	}
}

func TestEnvPositiveIntRejectsZeroAndOverflow(t *testing.T) {
	const fallback = 250
	for _, value := range []string{"0", "-1", "18446744073709551615", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TEST_LIMIT", value)
			if got := envPositiveInt("TEST_LIMIT", fallback); got != fallback {
				t.Fatalf("value %q produced %d, want fallback %d", value, got, fallback)
			}
		})
	}
	t.Setenv("TEST_LIMIT", "512")
	if got := envPositiveInt("TEST_LIMIT", fallback); got != 512 {
		t.Fatalf("valid positive limit produced %d", got)
	}
}
''')
