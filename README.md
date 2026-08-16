# aladdin-solver-engine

A CoW Protocol solver engine for **shadow competition**. It accepts auctions,
proposes settlement solutions and records driver feedback so the model can be
improved from evidence rather than guesswork.

The service implements the solver-engine surface from
`cowprotocol/services` (`crates/solvers/openapi.yml`):

- `POST /solve`
- `POST /notify`
- `GET /health` for local operation

See [ROADMAP.md](ROADMAP.md) for the governed development and graduation plan.

## Hard safety boundary

This service:

- holds no private key;
- opens no RPC connection;
- signs nothing;
- submits nothing on-chain;
- reads liquidity only from the auction payload supplied by the driver.

CI fails if Go code introduces common key, signing or transaction-submission
paths. A live solver would require a separate repository and security review;
it is not a hidden future mode of this process.

## Solving model

The engine performs two passes over each auction.

### Pass 1 — coincidence of wants

Opposite sell orders are paired when their limits cross. A pure CoW solution
uses no external liquidity and has the lowest interaction cost. When profitable
filtering is enabled, the combined native-value surplus must exceed estimated
gas before the match is returned.

### Pass 2 — bounded baseline routing

Remaining orders are quoted through liquidity supplied in the auction:

- direct routes;
- every bounded two-hop pool pair through the most connected auction tokens;
- deterministic tie-breaking by output, gas and route identity;
- no hardcoded base-token addresses.

Supported pool kinds:

- `constantProduct` — Uniswap V2-style exact-integer math;
- `concentratedLiquidity` — Uniswap V3-style tick-crossing math;
- `stable` — Balancer/Curve-style StableMath, including pools with more than
  two tokens and token scaling factors.

Malformed pools are rejected and counted. Unsupported kinds are never quoted
with an approximation.

Buy orders binary-search the smallest input that satisfies the requested output.
Sell orders use the highest-output route that respects the order limit.

## Arithmetic and determinism

Token amounts use `math/big`; `float64` never represents a token amount. Decimal
fees, amplification parameters and scaling factors are parsed as exact
rationals. Token ordering and equal-connectivity routing ties are deterministic,
so the same input can be replayed consistently.

## Unsupported settlement semantics

The current engine safely skips orders carrying behavior it does not yet encode:

- pre- or post-interaction hooks;
- wrapper calls;
- protocol fee policies;
- non-ERC20 sell-token sources or buy-token destinations;
- unknown order kinds.

Additional current gaps:

- no weighted-product pool quoting;
- no foreign limit-order liquidity;
- no optimized partial-fill selection;
- no multi-order batching across different token pairs;
- no inventory, JIT liquidity or CEX-DEX arbitrage;
- no authenticated winner/objective data, so the report does **not** claim a
  winner-beating rate.

These gaps are measured in the recorder output and prioritized according to
observed shadow coverage.

## Build and validate

Go 1.24 is pinned by `go.mod`.

```sh
make test
make race
make build
make ci
```

`make ci` is the single source of truth used by GitHub Actions. It runs:

- formatting validation;
- `go vet`;
- normal and race-enabled tests;
- command builds;
- the standard-library-only dependency boundary;
- the no-key/no-sign/no-submit boundary;
- workflow placement validation.

Install the same gates as a pre-push hook with:

```sh
make hooks
```

## Run locally

```sh
make run
```

Configuration is environment-only:

| Variable | Default | Meaning |
|---|---:|---|
| `LISTEN_ADDR` | `:8000` | HTTP bind address |
| `RECORD_DIR` | `./data` | Append-only JSONL evidence directory |
| `RECORD_FULL_AUCTIONS` | `false` | Retain full auctions for offline replay |
| `REQUIRE_PROFITABLE` | `true` | Drop candidates whose estimated edge does not cover gas |
| `SETTLEMENT_OVERHEAD_GAS` | `106000` | Fixed settlement gas estimate |
| `PER_TRADE_GAS` | `60000` | Marginal gas estimate per trade |
| `MAX_SOLUTIONS` | `40` | Maximum solutions returned per auction |
| `MAX_ORDERS` | `250` | Maximum eligible orders considered |
| `LOG_LEVEL` | `info` | Structured log level |

For a VPS shadow deployment, `deploy/solver.service` provides a hardened systemd
unit. Bind to `127.0.0.1` and run the driver on the same trusted host; this
service has no authentication and must not face the public internet.

## Evidence and reporting

The recorder writes daily append-only JSONL files for:

- auctions, solve latency, model stats and proposed solutions;
- notifications, including unknown extensible metadata sent by the driver.

Generate a report with:

```sh
make report
```

The report separates:

1. **coverage** — auctions for which any solution was produced;
2. **validation feedback** — success, simulation, clearing-price, timeout and
   other driver notification kinds;
3. **diagnosis** — unsupported orders, unavailable routes, unmet limits,
   unprofitable candidates and skipped liquidity.

Coverage alone is not proof of competitiveness. A winner-beating rate requires
winner or objective evidence bound to the same auctions; the current report
states that limitation explicitly.

## License

Unlicensed. All rights reserved.
