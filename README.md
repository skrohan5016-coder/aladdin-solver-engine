# aladdin-solver-engine

A CoW Protocol solver engine, built to run in **shadow competition** and
produce the one number that decides whether going live is worth anything:
how often it can actually solve a real auction.

It implements the solver-engine interface from
`cowprotocol/services` (`crates/solvers/openapi.yml`) — `POST /solve` and
`POST /notify`.

## What it will not do

This service **holds no private key, opens no RPC connection, signs nothing,
and submits nothing on-chain.** It receives an auction over HTTP and returns
proposed solutions. That is the whole surface.

All liquidity comes from the auction payload itself — the driver ships pool
state with every request — so no node, no archive access, no subscription.

CI enforces the boundary: any commit introducing a key, a signer, or a
transaction-submission path fails the build. If a later milestone needs those,
that is a different repository with a different review standard, not a quiet
commit here.

## The model

Two passes over each auction.

**Pass 1 — coincidence of wants.** If order A sells `sA` of token X wanting at
least `bA` of Y, and order B sells `sB` of Y wanting at least `bB` of X, the
two can be swapped outright exactly when `sB >= bA` and `sA >= bB`. The
clearing price vector `{X: sB, Y: sA}` balances the settlement by
construction. No external liquidity, no AMM hop, minimal gas — which is
precisely why these solutions score well when they exist.

**Pass 2 — baseline routing.** Every remaining order is routed through the
auction's own liquidity: direct pools first, then two-hop paths pivoting
through whichever tokens the auction's pool graph is most densely connected
around. No base-token addresses are hardcoded; the pivots are derived per
auction.

Supported pool kinds: `constantProduct` (Uniswap V2 and forks) and
`concentratedLiquidity` (Uniswap V3, full tick-crossing swap math). `stable`,
`weightedProduct`, and `limitOrder` are counted as skipped rather than
approximated — the report tells you how much liquidity you are leaving on the
table and therefore what to build next.

Buy orders binary-search the minimum input that satisfies the requested output,
so the user keeps the difference.

Solutions whose gas cost exceeds the surplus they generate are dropped by
default (`REQUIRE_PROFITABLE=true`). Bid quality is discounted by settlement
success rate, so bidding on batches you would not actually settle is not free.

### Arithmetic

No `float64` ever touches a token amount. Decimal fees from the wire (`"0.003"`)
are parsed into exact rationals. The V3 tick math is verified against
Uniswap's own reference values, including `MIN_TICK` and `MAX_TICK`.

## Known gaps

Stated plainly, because a solver that quietly mishandles these loses money
rather than auctions:

- Partial fills are not used; matches are full-amount only.
- Orders carrying pre/post-interaction hooks are skipped.
- Multi-order batching across different token pairs is not implemented — each
  solution settles one order, or one CoW pair.
- Stable and weighted pools are not quoted.
- No inventory, no JIT liquidity, no CEX-DEX arbitrage.

## Running it

```sh
make test          # full suite
make race          # race detector
make build         # ./bin/solver and ./bin/report
make run
```

Configuration is environment variables only:

| Variable | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:8000` | Bind address |
| `RECORD_DIR` | `./data` | Where auction records are written |
| `RECORD_FULL_AUCTIONS` | `false` | Store full payloads for offline replay (large) |
| `REQUIRE_PROFITABLE` | `true` | Drop solutions whose gas exceeds their surplus |
| `SETTLEMENT_OVERHEAD_GAS` | `106000` | Fixed settlement cost |
| `PER_TRADE_GAS` | `60000` | Marginal cost per trade |
| `MAX_SOLUTIONS` | `40` | Cap per auction |
| `MAX_ORDERS` | `250` | Cap per auction |
| `LOG_LEVEL` | `info` | |

Deploying on a VPS: `deploy/solver.service` is a hardened systemd unit. Bind to
`127.0.0.1` and put the driver on the same host; this service has no
authentication and should not face the internet.

## Reading the results

```sh
./bin/report -dir ./data
```

Reports coverage, solve latency, why orders were dropped, which pool kinds were
skipped, and what the driver said about each submitted solution.

**Coverage is the gate.** It is the share of auctions for which the engine
produced any solution at all. If coverage stays low, the routing model is the
problem. If coverage is high but the driver reports `simulationFailed` or
`invalidClearingPrices`, the settlement encoding is. Going live sooner fixes
neither.

## Where this sits

Shadow competition needs no KYC, no bond, no company, and no capital. Run it
for several weeks and read the report. Only if coverage and driver feedback
both look healthy does the question of a live solver become a real question —
and that question is mostly about capital, latency, and order flow, none of
which live in this repository.

## Licence

Unlicensed. All rights reserved.
