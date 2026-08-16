package solve

import (
	"encoding/json"
	"math/big"
	"sort"
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

	case "stable":
		var toks map[string]struct {
			Balance       string `json:"balance"`
			ScalingFactor string `json:"scalingFactor"`
		}
		if err := json.Unmarshal(l.Tokens, &toks); err != nil || len(toks) < 2 {
			return nil, errSkip
		}
		// The amplification parameter is a plain decimal on the wire; the
		// math wants it multiplied by AMP_PRECISION (1000).
		ampNum, ampDen, err := amm.ParseDecimalRational(l.AmplificationParameter)
		if err != nil || ampNum.Sign() <= 0 || ampDen.Sign() <= 0 {
			return nil, errSkip
		}
		ampRaw := new(big.Int).Mul(ampNum, big.NewInt(1000))
		ampRaw.Quo(ampRaw, ampDen)
		if ampRaw.Sign() <= 0 {
			return nil, errSkip
		}

		addrs := make([]string, 0, len(toks))
		for a := range toks {
			addrs = append(addrs, strings.ToLower(a))
		}
		sort.Strings(addrs) // deterministic token order across runs

		balances := make([]*big.Int, 0, len(addrs))
		scales := make([]*big.Int, 0, len(addrs))
		for _, a := range addrs {
			var raw struct{ Balance, ScalingFactor string }
			for k, v := range toks {
				if strings.EqualFold(k, a) {
					raw.Balance, raw.ScalingFactor = v.Balance, v.ScalingFactor
					break
				}
			}
			bal, ok := new(big.Int).SetString(raw.Balance, 10)
			if !ok || bal.Sign() <= 0 {
				return nil, errSkip
			}
			// Scaling factors are 18-decimal fixed point on the wire.
			sfNum, sfDen, err := amm.ParseDecimalRational(raw.ScalingFactor)
			if err != nil || sfNum.Sign() <= 0 {
				return nil, errSkip
			}
			sf := new(big.Int).Mul(sfNum, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
			sf.Quo(sf, sfDen)
			if sf.Sign() <= 0 {
				return nil, errSkip
			}
			balances = append(balances, bal)
			scales = append(scales, sf)
		}

		base.TokenList = addrs
		base.Balances = balances
		base.ScalingFactors = scales
		base.AmplificationRaw = ampRaw
		base.TokenA, base.TokenB = addrs[0], addrs[1]
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
