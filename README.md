# aladdin-solver-engine

A CoW Protocol solver engine for **shadow competition**. It accepts auctions,
proposes settlement solutions and records driver feedback so the model can be
improved from evidence rather than guesswork.

The service implements the solver-engine surface from the exact upstream source
snapshot recorded in [UPSTREAM.md](UPSTREAM.md):

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

CI rejects common key, signing, submission, outbound-client and helper-process
paths in production Go code. The hardened systemd unit binds to loopback and
denies non-loopback network traffic. A live solver would require a separate
repository and security review; it is not a hidden future mode of this process.

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
- context cancellation checks between bounded quotes;
- no hardcoded base-token addresses.

Supported pool kinds:

- `constantProduct` — Uniswap V2-style exact-integer math;
- `concentratedLiquidity` — Uniswap V3-style tick-crossing math;
- `stable` — Balancer/Curve-style StableMath, including pools with more than
  two tokens and token scaling factors.

Malformed pools are rejected and counted. Supplied liquidity, stable-pool token
count and V3 tick count are bounded before routing. Unsupported kinds are never
quoted with an approximation.

Buy orders binary-search the smallest input that satisfies the requested output.
Sell orders use the highest-output route that respects the order limit.

## Input and arithmetic rules

Token amounts use `math/big`; `float64` never represents a token amount. Decimal
fees, amplification parameters and scaling factors are parsed as exact
rationals. Token ordering and equal-connectivity routing ties are deterministic,
so the same input can be replayed consistently.

`/solve` rejects malformed JSON, multiple top-level values and duplicate object
keys at any depth. This prevents ambiguous payloads from being interpreted
differently by this Go service and the upstream Rust driver.

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

These gaps are measured in recorder output and prioritized according to observed
shadow coverage.

## Build and validate

Go `1.24.13` is pinned by `go.mod`. GitHub Actions are pinned by immutable commit
SHA and run on Ubuntu 24.04.

```sh
make test
make race
make build
make ci
```

`make ci` is the single source of truth used by GitHub Actions. It runs:

- formatting and module-tidiness validation;
- exact Go toolchain and upstream-contract pin checks;
- `go vet`;
- uncached normal and race-enabled tests;
- command builds;
- the standard-library-only dependency boundary;
- no-key/no-sign/no-submit and no-outbound-client boundaries;
- workflow-placement and deployment-network checks.

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
| `LISTEN_ADDR` | `127.0.0.1:8000` | Loopback HTTP bind address |
| `RECORD_DIR` | `./data` | Append-only JSONL evidence directory |
| `RECORD_FULL_AUCTIONS` | `false` | Retain full auctions with source, toolchain and config identity for offline replay |
| `REQUIRE_PROFITABLE` | `true` | Drop candidates whose estimated edge does not cover gas |
| `SETTLEMENT_OVERHEAD_GAS` | `106000` | Fixed settlement gas estimate |
| `PER_TRADE_GAS` | `60000` | Marginal gas estimate per trade |
| `MAX_SOLUTIONS` | `40` | Maximum solutions returned per auction |
| `MAX_ORDERS` | `250` | Maximum eligible orders considered |
| `MAX_POOLS` | `2048` | Maximum supplied liquidity entries parsed |
| `LOG_LEVEL` | `info` | Structured log level |

For a VPS shadow deployment, `deploy/solver.service` provides a hardened systemd
unit. It permits loopback HTTP to a trusted local driver, denies external network
egress and restricts filesystem writes to `/opt/solver/data`.

## Evidence and reporting

The recorder writes private, daily append-only JSONL files with explicit schema
identifiers for:

- auctions, solve latency, model stats and proposed solutions;
- notifications, including unknown extensible metadata sent by the driver.

Recorder open, encode, append, rotation and close failures are surfaced and
logged rather than silently discarded. Full-auction recording is opt-in because
it can include order signatures and consumes substantial disk space. It also
requires a binary built with an exact embedded source commit; `make build` does
this automatically from the current Git checkout.

Generate a report with:

```sh
make report
```

The report separates:

1. **coverage** — auctions for which any solution was returned;
2. **validation feedback** — success, simulation, clearing-price, timeout and
   other driver notification kinds;
3. **diagnosis** — unsupported orders, unavailable routes, unmet limits,
   unprofitable candidates and skipped liquidity;
4. **candidate versus returned solutions** — so the configured cap is explicit.

Latency percentiles use nearest-rank calculation and corrupt evidence causes a
non-zero report failure instead of being skipped.

Coverage alone is not proof of competitiveness. A winner-beating rate requires
winner or objective evidence bound to the same auctions; the current report
states that limitation explicitly.

## Offline corpus and deterministic replay

Full-auction JSONL evidence can be sealed into a new immutable corpus:

```sh
make pack-corpus RECORDS='./data/auctions-*.jsonl' CORPUS=./private-corpus
```

The packer verifies the recorded result before publishing, binds every file by
SHA-256, records the exact engine commit, Go toolchain, upstream contract and
resolved solver configuration, writes the manifest last, and refuses existing
or symlinked destinations. Signatures are redacted by default only after proving
that redaction does not change solver output.

Replay the corpus entirely offline with:

```sh
make replay-corpus CORPUS=./private-corpus
```

The replay command rejects partial records, unknown inventory, missing or extra
files, symlinks, oversized files, digest mismatch, source/config/toolchain drift,
and any changed solution or statistic. Its JSON report is deterministic for the
same accepted corpus. See [docs/PHASE2_OFFLINE_REPLAY.md](docs/PHASE2_OFFLINE_REPLAY.md).

## License

Unlicensed. All rights reserved.
