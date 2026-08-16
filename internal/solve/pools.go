package solve

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/amm"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
)

// BuildPools converts the auction's liquidity array into routable pools.
// Unsupported kinds are skipped and counted, never guessed at.
func BuildPools(liq []api.Liquidity) ([]*amm.Pool, map[string]int) {
	pools := make([]*amm.Pool, 0, len(liq))
	skipped := map[string]int{}
	for i := range liq {
		p, err := buildPool(&liq[i])
		if err != nil || p == nil {
			kind := liq[i].Kind
			if kind == "" {
				kind = "unknown"
			}
			skipped[kind]++
			continue
		}
		pools = append(pools, p)
	}
	return pools, skipped
}

func buildPool(l *api.Liquidity) (*amm.Pool, error) {
	feeNum, feeDen, err := amm.ParseDecimalRational(l.Fee)
	if err != nil {
		return nil, err
	}
	gas, _ := strconv.ParseUint(l.GasEstimate, 10, 64)
	if gas == 0 {
		gas = 90_000
	}
	base := &amm.Pool{
		ID:          l.ID,
		Kind:        l.Kind,
		Address:     l.Address,
		GasEstimate: gas,
		FeeNum:      feeNum,
		FeeDen:      feeDen,
	}

	switch l.Kind {
	case "constantProduct":
		var toks map[string]api.TokenReserve
		if err := json.Unmarshal(l.Tokens, &toks); err != nil {
			return nil, err
		}
		if len(toks) != 2 {
			return nil, errSkip
		}
		addrs := make([]string, 0, 2)
		for a := range toks {
			addrs = append(addrs, strings.ToLower(a))
		}
		if addrs[1] < addrs[0] {
			addrs[0], addrs[1] = addrs[1], addrs[0]
		}
		ra, oka := new(big.Int).SetString(reserveOf(toks, addrs[0]), 10)
		rb, okb := new(big.Int).SetString(reserveOf(toks, addrs[1]), 10)
		if !oka || !okb || ra.Sign() <= 0 || rb.Sign() <= 0 {
			return nil, errSkip
		}
		base.TokenA, base.TokenB = addrs[0], addrs[1]
		base.ReserveA, base.ReserveB = ra, rb
		return base, nil

	case "concentratedLiquidity":
		var toks []string
		if err := json.Unmarshal(l.Tokens, &toks); err != nil || len(toks) != 2 {
			return nil, errSkip
		}
		sp, ok1 := new(big.Int).SetString(l.SqrtPrice, 10)
		lq, ok2 := new(big.Int).SetString(l.Liquidity, 10)
		if !ok1 || !ok2 || l.Tick == nil || sp.Sign() <= 0 {
			return nil, errSkip
		}
		ticks := make([]amm.Tick, 0, len(l.LiquidityNet))
		for k, v := range l.LiquidityNet {
			idx, err := strconv.ParseInt(k, 10, 32)
			if err != nil {
				continue
			}
			net, ok := new(big.Int).SetString(v, 10)
			if !ok {
				continue
			}
			ticks = append(ticks, amm.Tick{Index: int32(idx), Net: net})
		}
		amm.SortTicks(ticks)
		base.TokenA, base.TokenB = strings.ToLower(toks[0]), strings.ToLower(toks[1])
		base.SqrtPriceX96, base.Liquidity, base.Tick, base.Ticks = sp, lq, *l.Tick, ticks
		return base, nil
	}
	return nil, errSkip
}

func reserveOf(m map[string]api.TokenReserve, addr string) string {
	for k, v := range m {
		if strings.EqualFold(k, addr) {
			return v.Balance
		}
	}
	return "0"
}

type skipErr struct{}

func (skipErr) Error() string { return "skip" }

var errSkip = skipErr{}
