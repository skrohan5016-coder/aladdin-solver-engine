# Aladdin Solver Engine Roadmap

This repository is a **shadow-only CoW Protocol solver engine**. It receives an
auction from a trusted local driver, proposes solutions, records the outcome,
and stops there. It must not hold keys, connect to an RPC endpoint, sign, or
submit transactions.

The roadmap is evidence-driven: each phase has a measurable exit gate. A phase
is not complete because code exists; it is complete when its exact commit passes
the stated tests and produces the stated evidence.

## Phase 0 — Recover a trustworthy baseline

Status: in progress.

Deliverables:

- a root `.github/workflows/ci.yml` that runs the same `scripts/ci.sh` gates as
  local development;
- compiling, deterministic V2, V3 and Balancer-style stable-pool quoting;
- safe rejection of malformed liquidity and unsupported order semantics;
- append-only auction and notification evidence with extensible notification
  metadata preserved;
- an honest report that separates coverage, validation feedback and unknowns.

Exit gate:

- `gofmt`, `go vet`, `go test`, `go test -race` and both command builds pass on
  the exact pull-request head;
- the shadow boundary and standard-library-only boundary pass;
- no active workflow file exists outside `.github/workflows`;
- the pull request documents all known unsupported behavior.

## Phase 1 — Pin and continuously verify the CoW wire contract

Deliverables:

- record the exact upstream `cowprotocol/services` commit used for the solver
  API contract;
- retain representative auction, solution and notification fixtures;
- add compatibility tests for every consumed field and every emitted field;
- detect upstream schema drift before shadow deployment;
- reject a payload when ignoring a field could change settlement semantics.

Exit gate:

- fixture replay is deterministic byte-for-byte;
- every emitted solution validates against the pinned contract;
- optional upstream notification metadata survives record and replay.

## Phase 2 — Build a reproducible offline replay corpus

Deliverables:

- opt-in full-auction recording with manifest, file hashes and run identity;
- a replay command that reproduces solutions and stats from recorded input;
- bounded resource use for large auctions and malformed payloads;
- deterministic ordering independent of Go map iteration;
- corpus redaction and retention rules.

Exit gate:

- the same corpus produces identical normalized solutions and reports across
  repeated runs on the pinned Go toolchain;
- interrupted or corrupt records fail closed and are reported explicitly.

## Phase 3 — Establish shadow quality gates

Run the engine beside a CoW driver without any signing or submission ability.
Collect enough auctions to measure:

- auction coverage;
- p50, p95 and maximum solve latency;
- driver `success`, `simulationFailed`, `invalidClearingPrices`, timeout and
  other notification rates;
- unsupported order and liquidity frequency;
- solution objective value when the driver exposes sufficient evidence.

Initial graduation targets must be set from observed data, not invented in
advance. The trial report must state the sample size, date range, chain, driver
version, engine commit and configuration.

Exit gate:

- a reviewed shadow report demonstrates stable latency and validation behavior;
- no result is described as a winner-beating rate unless winner/objective data
  are actually available and bound to the same auctions.

## Phase 4 — Expand coverage by measured opportunity

Implement the largest observed coverage gaps in descending evidence order.
Candidates include:

- weighted-product pools;
- external limit-order liquidity;
- partial-fill optimization;
- pre/post interactions and wrapper calls;
- protocol fee policies;
- multi-order batching and better CoW matching;
- three-or-more-hop search with explicit time and memory budgets.

Each addition requires reference vectors, adversarial tests, replay evidence and
an updated unsupported-behavior section. Approximation is not an acceptable
substitute for settlement-compatible arithmetic.

## Phase 5 — Competitive scoring and optimization

Only after correctness and coverage are stable:

- rank candidates by the driver's objective model rather than raw token output;
- account for gas, protocol fees, success probability and settlement overhead;
- retain rejected candidates and reason codes for offline analysis;
- compare against winner or ranking evidence only where the driver exposes it.

Exit gate:

- scoring parity tests against the pinned driver contract;
- reproducible evidence that optimization improves objective value without
  reducing simulation validity.

## Phase 6 — Separate live-execution decision

Going live is not an extension of this repository. It requires a separate
security boundary and review covering keys, signer isolation, capital, bonding,
RPC trust, transaction submission, monitoring and emergency shutdown.

This repository remains useful as the unprivileged model and evidence producer.
No live capability may be added here through an ordinary feature commit.

## Development and landing policy

- Work on a non-default branch and use a pull request.
- Do not force-push published review history.
- Do not merge while exact-head checks are pending or failing.
- A human must explicitly approve the exact final head before landing.
- Never weaken a gate to make a change pass; repair the implementation or make
  the unsupported behavior explicit.
