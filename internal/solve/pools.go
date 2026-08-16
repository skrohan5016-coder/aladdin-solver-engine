package solve

import (
	"context"
	"encoding/json"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/amm"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
)

const (
	maxStablePoolTokens  = 64
	maxConcentratedTicks = 16_384
	maxWireDecimalLength = 128
)

func BuildPools(liquidity []api.Liquidity) ([]*amm.Pool, map[string]int) {
	return BuildPoolsContext(context.Background(), liquidity, 0)
}

func BuildPoolsContext(ctx context.Context, liquidity []api.Liquidity, maxPools int) ([]*amm.Pool, map[string]int) {
	limit := len(liquidity)
	skipped := map[string]int{}
	if maxPools > 0 && limit > maxPools {
		limit = maxPools
		skipped["resourceLimit"] += len(liquidity) - limit
	}

	pools := make([]*amm.Pool, 0, limit)
	for i := 0; i < limit; i++ {
		if ctx.Err() != nil {
			skipped["cancelled"] += limit - i
			break
		}
		pool, err := buildPool(&liquidity[i])
		if err != nil || pool == nil {
			kind := liquidity[i].Kind
			if kind == "" {
				kind = "unknown"
			}
			skipped[kind]++
			continue
		}
		pools = append(pools, pool)
	}
	return pools, skipped
}

func buildPool(liquidity *api.Liquidity) (*amm.Pool, error) {
	trimmedFee := strings.TrimSpace(liquidity.Fee)
	if trimmedFee == "" || trimmedFee == "-" || trimmedFee == "." || trimmedFee == "-." ||
		len(trimmedFee) > maxWireDecimalLength {
		return nil, errSkip
	}
	feeNum, feeDen, err := amm.ParseDecimalRational(trimmedFee)
	if err != nil || feeDen.Sign() <= 0 || feeNum.Sign() < 0 || feeNum.Cmp(feeDen) >= 0 ||
		feeNum.BitLen() > 256 || feeDen.BitLen() > 256 {
		return nil, errSkip
	}
	if !decimalDigits(liquidity.GasEstimate, 20) {
		return nil, errSkip
	}
	gas, err := strconv.ParseUint(liquidity.GasEstimate, 10, 64)
	if err != nil {
		return nil, errSkip
	}
	if gas == 0 {
		gas = 90_000
	}
	base := &amm.Pool{
		ID:          liquidity.ID,
		Kind:        liquidity.Kind,
		Address:     strings.ToLower(liquidity.Address),
		GasEstimate: gas,
		FeeNum:      feeNum,
		FeeDen:      feeDen,
	}

	switch liquidity.Kind {
	case "constantProduct":
		var tokens map[string]api.TokenReserve
		if err := json.Unmarshal(liquidity.Tokens, &tokens); err != nil {
			return nil, err
		}
		if len(tokens) != 2 {
			return nil, errSkip
		}
		addresses := make([]string, 0, 2)
		seen := map[string]bool{}
		for address := range tokens {
			normalized := strings.ToLower(address)
			if seen[normalized] {
				return nil, errSkip
			}
			seen[normalized] = true
			addresses = append(addresses, normalized)
		}
		sort.Strings(addresses)
		reserveA, okA := parsePositiveUnsigned(reserveOf(tokens, addresses[0]), 256)
		reserveB, okB := parsePositiveUnsigned(reserveOf(tokens, addresses[1]), 256)
		if !okA || !okB {
			return nil, errSkip
		}
		base.TokenA, base.TokenB = addresses[0], addresses[1]
		base.ReserveA, base.ReserveB = reserveA, reserveB
		return base, nil

	case "concentratedLiquidity":
		var tokens []string
		if err := json.Unmarshal(liquidity.Tokens, &tokens); err != nil || len(tokens) != 2 {
			return nil, errSkip
		}
		tokens[0], tokens[1] = strings.ToLower(tokens[0]), strings.ToLower(tokens[1])
		if tokens[0] == tokens[1] || len(liquidity.LiquidityNet) > maxConcentratedTicks {
			return nil, errSkip
		}
		sqrtPrice, okPrice := parsePositiveUnsigned(liquidity.SqrtPrice, 256)
		liquidityAmount, okLiquidity := parsePositiveUnsigned(liquidity.Liquidity, 128)
		if !okPrice || !okLiquidity || liquidity.Tick == nil ||
			*liquidity.Tick < amm.MinTick || *liquidity.Tick > amm.MaxTick {
			return nil, errSkip
		}
		ticks := make([]amm.Tick, 0, len(liquidity.LiquidityNet))
		seenTicks := make(map[int32]struct{}, len(liquidity.LiquidityNet))
		for rawIndex, rawNet := range liquidity.LiquidityNet {
			index, err := strconv.ParseInt(rawIndex, 10, 32)
			if err != nil || index < int64(amm.MinTick) || index > int64(amm.MaxTick) {
				return nil, errSkip
			}
			parsedIndex := int32(index)
			if _, duplicate := seenTicks[parsedIndex]; duplicate {
				return nil, errSkip
			}
			seenTicks[parsedIndex] = struct{}{}
			net, ok := parseSignedInteger(rawNet, 128)
			if !ok {
				return nil, errSkip
			}
			ticks = append(ticks, amm.Tick{Index: parsedIndex, Net: net})
		}
		amm.SortTicks(ticks)
		base.TokenA, base.TokenB = tokens[0], tokens[1]
		base.SqrtPriceX96 = sqrtPrice
		base.Liquidity = liquidityAmount
		base.Tick = *liquidity.Tick
		base.Ticks = ticks
		return base, nil

	case "stable":
		var tokens map[string]struct {
			Balance       string `json:"balance"`
			ScalingFactor string `json:"scalingFactor"`
		}
		if err := json.Unmarshal(liquidity.Tokens, &tokens); err != nil ||
			len(tokens) < 2 || len(tokens) > maxStablePoolTokens {
			return nil, errSkip
		}
		ampText := strings.TrimSpace(liquidity.AmplificationParameter)
		if ampText == "" || ampText == "-" || ampText == "." || ampText == "-." ||
			len(ampText) > maxWireDecimalLength {
			return nil, errSkip
		}
		ampNum, ampDen, err := amm.ParseDecimalRational(ampText)
		if err != nil || ampNum.Sign() <= 0 || ampDen.Sign() <= 0 {
			return nil, errSkip
		}
		ampRaw := new(big.Int).Mul(ampNum, big.NewInt(1000))
		ampRaw.Quo(ampRaw, ampDen)
		if ampRaw.Sign() <= 0 || ampRaw.BitLen() > 256 {
			return nil, errSkip
		}

		addresses := make([]string, 0, len(tokens))
		seen := map[string]bool{}
		for address := range tokens {
			normalized := strings.ToLower(address)
			if seen[normalized] {
				return nil, errSkip
			}
			seen[normalized] = true
			addresses = append(addresses, normalized)
		}
		sort.Strings(addresses)

		balances := make([]*big.Int, 0, len(addresses))
		scalingFactors := make([]*big.Int, 0, len(addresses))
		for _, address := range addresses {
			var raw struct{ Balance, ScalingFactor string }
			for key, value := range tokens {
				if strings.EqualFold(key, address) {
					raw.Balance = value.Balance
					raw.ScalingFactor = value.ScalingFactor
					break
				}
			}
			balance, ok := parsePositiveUnsigned(raw.Balance, 256)
			scaleText := strings.TrimSpace(raw.ScalingFactor)
			if !ok || scaleText == "" || scaleText == "-" || scaleText == "." || scaleText == "-." ||
				len(scaleText) > maxWireDecimalLength {
				return nil, errSkip
			}
			scaleNum, scaleDen, err := amm.ParseDecimalRational(scaleText)
			if err != nil || scaleNum.Sign() <= 0 || scaleDen.Sign() <= 0 {
				return nil, errSkip
			}
			scale := new(big.Int).Mul(scaleNum, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
			scale.Quo(scale, scaleDen)
			if scale.Sign() <= 0 || scale.BitLen() > 256 {
				return nil, errSkip
			}
			balances = append(balances, balance)
			scalingFactors = append(scalingFactors, scale)
		}

		base.TokenList = addresses
		base.Balances = balances
		base.ScalingFactors = scalingFactors
		base.AmplificationRaw = ampRaw
		base.TokenA, base.TokenB = addresses[0], addresses[1]
		return base, nil
	}
	return nil, errSkip
}

func reserveOf(tokens map[string]api.TokenReserve, address string) string {
	for key, value := range tokens {
		if strings.EqualFold(key, address) {
			return value.Balance
		}
	}
	return "0"
}

func decimalDigits(raw string, maxDigits int) bool {
	if raw == "" || len(raw) > maxDigits {
		return false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return false
		}
	}
	return true
}

func parsePositiveUnsigned(raw string, bits int) (*big.Int, bool) {
	maxDigits := (bits*30103)/100000 + 1
	if !decimalDigits(raw, maxDigits) {
		return nil, false
	}
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok || value.Sign() <= 0 || value.BitLen() > bits {
		return nil, false
	}
	return value, true
}

func parseSignedInteger(raw string, bits int) (*big.Int, bool) {
	if raw == "" || len(raw) > 1+(bits*30103)/100000+1 {
		return nil, false
	}
	digits := raw
	if raw[0] == '-' {
		digits = raw[1:]
	}
	if !decimalDigits(digits, len(digits)) {
		return nil, false
	}
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return nil, false
	}
	limit := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
	min := new(big.Int).Neg(new(big.Int).Set(limit))
	max := new(big.Int).Sub(new(big.Int).Set(limit), big.NewInt(1))
	if value.Cmp(min) < 0 || value.Cmp(max) > 0 {
		return nil, false
	}
	return value, true
}

type skipErr struct{}

func (skipErr) Error() string { return "skip" }

var errSkip = skipErr{}
