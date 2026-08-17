#!/usr/bin/env python3
from __future__ import annotations
import argparse, json
from pathlib import Path

Q96 = 1 << 96
AMP_PRECISION = 1000
ONE = 10**18
TICK_FACTORS = [
    int(x, 16) for x in (
        "fffcb933bd6fad37aa2d162d1a594001", "fff97272373d413259a46990580e213a",
        "fff2e50f5f656932ef12357cf3c7fdcc", "ffe5caca7e10e4e61c3624eaa0941cd0",
        "ffcb9843d60f6159c9db58835c926644", "ff973b41fa98c081472e6896dfb254c0",
        "ff2ea16466c96a3843ec78b326b52861", "fe5dee046a99a2a811c461f1969c3053",
        "fcbe86c7900a88aedcffc83b479aa3a4", "f987a7253ac413176f2b074cf7815e54",
        "f3392b0822b70005940c7a398e4b70f3", "e7159475a2c29b7443b29c7fa6e889d9",
        "d097f3bdfd2022b8845ad8f792aa5825", "a9f746462d870fdf8a65dc1f90e061e5",
        "70d869a156d2a1b890bb3df62baf32f7", "31be135f97d08fd981231505542fcfa6",
        "9aa508b5b7a84e1c677de54f3e99bc9", "5d6af8dedb81196699c329225ee604",
        "2216e584f5fa1ea926041bedfe98", "48a170391f7dc42444e8fa2",
    )
]
MAX_U256 = (1 << 256) - 1

def div_up(a: int, b: int) -> int:
    return (a + b - 1) // b

def tick_ratio(tick: int) -> int:
    absolute = abs(tick)
    ratio = TICK_FACTORS[0] if absolute & 1 else 1 << 128
    for i in range(1, len(TICK_FACTORS)):
        if absolute & (1 << i):
            ratio = (ratio * TICK_FACTORS[i]) >> 128
    if tick > 0:
        ratio = MAX_U256 // ratio
    return (ratio >> 32) + (1 if ratio & ((1 << 32) - 1) else 0)

def cp_out(reserve_in: int, reserve_out: int, amount_in: int, fee_num: int, fee_den: int) -> int:
    after = amount_in * (fee_den - fee_num)
    return after * reserve_out // (reserve_in * fee_den + after)

def amount0_delta(a: int, b: int, liquidity: int, round_up: bool) -> int:
    if a > b:
        a, b = b, a
    n1 = liquidity << 96
    n2 = b - a
    if round_up:
        return div_up(div_up(n1 * n2, b), a)
    return (n1 * n2 // b) // a

def amount1_delta(a: int, b: int, liquidity: int, round_up: bool) -> int:
    if a > b:
        a, b = b, a
    n = liquidity * (b - a)
    return div_up(n, Q96) if round_up else n // Q96

def v3_single_range(sqrt_price: int, liquidity: int, amount_in: int, fee_pips: int, zero_for_one: bool) -> int:
    less_fee = amount_in * (1_000_000 - fee_pips) // 1_000_000
    if zero_for_one:
        numerator = liquidity << 96
        next_price = div_up(numerator * sqrt_price, numerator + less_fee * sqrt_price)
        used = amount0_delta(next_price, sqrt_price, liquidity, True)
        assert used <= amount_in
        return amount1_delta(next_price, sqrt_price, liquidity, False)
    next_price = sqrt_price + less_fee * Q96 // liquidity
    used = amount1_delta(sqrt_price, next_price, liquidity, True)
    assert used <= amount_in
    return amount0_delta(sqrt_price, next_price, liquidity, False)

def stable_invariant(amp: int, balances: list[int]) -> int:
    n = len(balances)
    total = sum(balances)
    if total == 0:
        return 0
    invariant = total
    amp_times_total = amp * n
    for _ in range(255):
        d_p = invariant
        for balance in balances:
            d_p = d_p * invariant // (balance * n)
        previous = invariant
        numerator = ((amp_times_total * total // AMP_PRECISION) + d_p * n) * invariant
        denominator = ((amp_times_total - AMP_PRECISION) * invariant // AMP_PRECISION) + (n + 1) * d_p
        invariant = numerator // denominator
        if abs(invariant - previous) <= 1:
            return invariant
    raise RuntimeError("invariant did not converge")

def stable_balance(amp: int, balances: list[int], invariant: int, token_index: int) -> int:
    n = len(balances)
    amp_times_total = amp * n
    total = balances[0]
    p_d = total * n
    for balance in balances[1:]:
        p_d = p_d * balance * n // invariant
        total += balance
    total -= balances[token_index]
    inv2 = invariant * invariant
    c = div_up(inv2, amp_times_total * p_d) * AMP_PRECISION * balances[token_index]
    b = total + (invariant // amp_times_total) * AMP_PRECISION
    token_balance = div_up(inv2 + c, invariant + b)
    for _ in range(255):
        previous = token_balance
        token_balance = div_up(token_balance * token_balance + c, token_balance * 2 + b - invariant)
        if abs(token_balance - previous) <= 1:
            return token_balance
    raise RuntimeError("balance did not converge")

def stable_out(balances_raw: list[int], scales: list[int], amp: int, fee_num: int, fee_den: int, token_in: int, token_out: int, amount_in: int) -> int:
    fee_bfp = fee_num * ONE // fee_den
    fee = div_up(amount_in * fee_bfp, ONE)
    after = amount_in - fee
    balances = [b * s // ONE for b, s in zip(balances_raw, scales)]
    up_in = after * scales[token_in] // ONE
    invariant = stable_invariant(amp, balances)
    balances[token_in] += up_in
    final_out = stable_balance(amp, balances, invariant, token_out)
    balances[token_in] -= up_in
    up_out = balances[token_out] - final_out - 1
    return up_out * ONE // scales[token_out]

def build() -> dict:
    cp = [
        dict(reserveIn="1000000", reserveOut="1000000", amountIn="1000", feeNum="3", feeDen="1000"),
        dict(reserveIn="100000000000000000000", reserveOut="200000000000000000000000", amountIn="1000000000000000000", feeNum="3", feeDen="1000"),
    ]
    for v in cp:
        v["amountOut"] = str(cp_out(*map(int, [v["reserveIn"], v["reserveOut"], v["amountIn"], v["feeNum"], v["feeDen"]])))
    ticks = [-887272, -1, 0, 1, 887272]
    tick_vectors = [{"tick": t, "sqrtPriceX96": str(tick_ratio(t))} for t in ticks]
    v3 = [{
        "sqrtPriceX96": str(Q96),
        "liquidity": "100000000000000000000",
        "amountIn": "1000000000000000000",
        "feePips": 3000,
        "zeroForOne": True,
    }]
    for v in v3:
        v["amountOut"] = str(v3_single_range(int(v["sqrtPriceX96"]), int(v["liquidity"]), int(v["amountIn"]), v["feePips"], v["zeroForOne"]))
    stable = [{
        "balances": ["1000000000000000000000000", "1000000000000000000000000"],
        "scalingFactors": [str(ONE), str(ONE)],
        "amplificationRaw": "100000",
        "feeNum": "4",
        "feeDen": "10000",
        "tokenIn": 0,
        "tokenOut": 1,
        "amountIn": "1000000000000000000",
    }]
    for v in stable:
        v["amountOut"] = str(stable_out(list(map(int, v["balances"])), list(map(int, v["scalingFactors"])), int(v["amplificationRaw"]), int(v["feeNum"]), int(v["feeDen"]), v["tokenIn"], v["tokenOut"], int(v["amountIn"])))
    return {
        "schema": "aladdin-pool-reference-v1",
        "sources": {
            "constantProduct": "Uniswap V2 getAmountOut integer formula",
            "concentratedLiquidity": "Uniswap V3 TickMath and single-range SwapMath integer formula",
            "stable": "cowprotocol/services pinned Balancer V2 stable_math.rs integer formula",
        },
        "constantProduct": cp,
        "tickMath": tick_vectors,
        "concentratedLiquidity": v3,
        "stable": stable,
    }

def encoded() -> bytes:
    return (json.dumps(build(), sort_keys=True, indent=2) + "\n").encode()

def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--path", default="testdata/reference/pool-vectors-v1.json")
    parser.add_argument("--write", action="store_true")
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    path = Path(args.path)
    data = encoded()
    if args.write:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    if args.check:
        if not path.exists() or path.read_bytes() != data:
            raise SystemExit(f"{path} is stale; run {Path(__file__).name} --write")
    if not args.write and not args.check:
        print(data.decode(), end="")
    return 0
if __name__ == "__main__":
    raise SystemExit(main())
