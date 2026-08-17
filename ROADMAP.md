# Aladdin Solver Engine Roadmap

This repository is a **shadow-only CoW Protocol solver engine**. It receives an
auction from a trusted local driver, proposes solutions, records the outcome,
and stops there. It must not hold keys, connect to an RPC endpoint, sign, or
submit transactions.

The roadmap is evidence-driven: a phase is not complete because code exists; it
is complete when its exact commit passes the stated tests and produces the
stated evidence. The reviewed upstream source contract is pinned in
[UPSTREAM.md](UPSTREAM.md).

## Phase 0 — Recover a trustworthy baseline

Status: accepted and squash-merged as `27d0800324358c39f36f240b0fbd5920faf5ee67`.

Deliverables:

- a root `.github/workflows/ci.yml` using immutable action SHAs and the same
  `scripts/ci.sh` gates as local development;
- a pinned Go patch toolchain and Ubuntu runner;
- compiling, deterministic V2, V3 and Balancer-style stable-pool quoting;
- bounded liquidity parsing, deadline cancellation and duplicate-key rejection;
- safe rejection of malformed liquidity and unsupported order semantics;
- append-only, private, schema-versioned auction and notification evidence;
- surfaced recorder failures instead of silent evidence loss;
- an honest report separating coverage, validation feedback, candidate count,
  returned solutions and unknowns;
- loopback-only deployment with external network egress denied.

Exit gate:

- `gofmt`, module tidiness, `go vet`, uncached tests, race tests and both command
  builds pass on the exact pull-request head and pull-request merge ref;
- the standard-library-only, no-key/no-sign/no-submit, no-outbound-client,
  workflow-placement, upstream-pin and deployment-network gates pass;
- fresh correctness, adversarial-security and reproducibility reviews have no
  unresolved actionable finding;
- the pull request documents all known unsupported behavior;
- Rohan explicitly approves the exact final head before landing.

## Phase 1 — Continuously verify the CoW wire contract

Status: accepted and squash-merged as `b6d5f1518eb19798921521d54c8bd9a4c1d6ecd8`.

Foundation already delivered in Phase 0:

- exact `cowprotocol/services` commit and authoritative file blob SHAs;
- strict numeric liquidity IDs and required collection validation;
- extensible notification metadata preservation;
- rejection of duplicate JSON keys at every depth.

Delivered in Phase 1:

- retained representative auction, solution and notification fixtures from the
  pinned contract;
- compatibility tests for consumed and emitted fields, including runtime scalar
  bounds and notification variant payloads;
- upstream schema-drift detection before shadow deployment;
- additional cross-language reference vectors for every pool arithmetic
  implementation;
- rejection of newly introduced fields when ignoring them could change
  settlement semantics.

Exit gate:

- fixture replay is deterministic byte-for-byte after normalization;
- every emitted solution validates against the pinned contract;
- optional upstream notification metadata survives record and replay;
- moving the pin requires a dedicated reviewed pull request;
- the exact final PR head and GitHub merge ref pass CI before Rohan approves the
  exact SHA for landing.

## Phase 2 — Build a reproducible offline replay corpus

Status: active on `agent/phase2-offline-replay-v1`.

Deliverables:

- opt-in full-auction recording with manifest, file hashes, engine commit,
  toolchain identity and configuration identity;
- a replay command that reproduces solutions and stats from recorded input;
- bounded resource use for large auctions and malformed payloads;
- deterministic ordering independent of Go map iteration;
- corpus redaction, retention and corruption-detection rules.

Exit gate:

- the same corpus produces identical normalized solutions and reports across
  repeated runs on the pinned toolchain;
- interrupted, partial or corrupt records fail closed and are reported;
- replay evidence identifies the exact engine source and configuration.

## Phase 3 — Establish shadow quality gates

Run the engine beside a local CoW driver without signing or submission ability.
Collect enough auctions to measure:

- auction coverage;
- p50, p95 and maximum solve latency;
- driver `success`, `simulationFailed`, `invalidClearingPrices`, timeout and
  other notification rates;
- unsupported order and liquidity frequency;
- candidate truncation frequency;
- solution objective value only when the driver exposes sufficient evidence.

Graduation targets must be set from observed data, not invented in advance. The
trial report must state sample size, date range, chain, driver version, engine
commit, upstream pin and configuration.

Exit gate:

- a reviewed shadow report demonstrates stable latency and validation behavior;
- no result is described as a winner-beating rate unless winner/objective data
  are actually available and bound to the same auctions.

## Phase 4 — Expand coverage by measured opportunity

Implement the largest observed coverage gaps in descending evidence order.
Candidates include weighted-product pools, external limit-order liquidity,
partial-fill optimization, hooks and wrappers, protocol fees, multi-order
batching, and deeper bounded routing.

Each addition requires reference vectors, adversarial tests, replay evidence and
an updated unsupported-behavior section. Approximation is not an acceptable
substitute for settlement-compatible arithmetic.

## Phase 5 — Competitive scoring and optimization

Only after correctness and coverage are stable, rank candidates by the driver's
objective model, account for gas and protocol fees, retain rejection evidence,
and compare against winner data only when the driver actually exposes it.

## Phase 6 — Separate live-execution decision

Going live is not an extension of this repository. It requires a separate
security boundary and review covering keys, signer isolation, capital, bonding,
RPC trust, transaction submission, monitoring and emergency shutdown.

## Development and landing policy

- Work on a non-default branch and use a pull request.
- Do not force-push published review history.
- Do not merge while exact-head or merge-ref checks are pending or failing.
- A human must explicitly approve the exact final head before landing.
- Never weaken a gate to make a change pass; repair the implementation or make
  the unsupported behavior explicit.
