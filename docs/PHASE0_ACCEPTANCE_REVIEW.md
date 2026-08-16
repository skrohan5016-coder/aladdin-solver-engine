# Phase 0 Adversarial Acceptance Review

Date: 2026-08-16

Repository: `skrohan5016-coder/aladdin-solver-engine`

Base reviewed: `8b77b29d86de3bd828e84dfd4178b2ad17143f93`

Reviewed implementation head: `3a28a5c975475cd75a3d37a18e7050b01620dc07`

Pinned CoW contract: `cowprotocol/services@20b3a62f222ad278502fb7e85cae4938e7f26f65`

This review covers the recovery baseline only. It does not authorize a live
solver, signing, transaction submission, capital deployment or a merge.

## Review scope 1 — arithmetic and route correctness

The review re-checked constant-product, concentrated-liquidity and stable-pool
quoting, route construction, gas accounting and deadline handling.

Remediations completed:

- concentrated-liquidity exact-input quotes now fail closed unless the complete
  declared input is consumed;
- a quote at an exhausted directional price boundary is rejected;
- V3 fees that are not exactly representable in one-millionth pips are rejected
  rather than silently rounded;
- two-hop pool gas and total settlement gas use checked `uint64` addition;
- context cancellation is checked after bounded pool quotes and before a route
  is returned;
- regression tests cover unconsumed exact input and gas overflow paths.

The deliberate coverage limit remains: a V3 quote requiring more than the
bounded tick-walk budget is rejected instead of approximated.

## Review scope 2 — CoW wire and semantic integrity

The implementation is bound to the pinned Solver Engine OpenAPI contract.
Liquidity IDs are opaque on auction input, but a returned liquidity interaction
requires a JSON number. Therefore:

- unsupported liquidity kinds may retain opaque IDs and are skipped;
- liquidity kinds the engine can route require canonical decimal `uint64` IDs;
- routable numeric IDs must be unique;
- case-insensitive duplicate token addresses and order UIDs are rejected;
- duplicate JSON object keys and trailing top-level JSON remain rejected;
- unknown notification metadata remains preserved for evidence and replay.

No unsupported settlement behavior is silently encoded. Orders with hooks,
wrappers, fee policies or unsupported balance sources/destinations remain
skipped.

## Review scope 3 — operational and evidence boundary

The process remains shadow-only.

Remediations completed:

- runtime HTTP binding is restricted to loopback addresses;
- recorder initialization is mandatory, so the service cannot silently run
  without evidence;
- integer environment limits reject zero, malformed and platform-overflowing
  values and retain safe defaults;
- the hardened systemd unit continues to deny external egress;
- CI continues to reject key, signing, submission, outbound-client and
  process-execution paths.

## Governed acceptance gates

The shared `scripts/ci.sh` contract requires all of the following on the exact
candidate tree:

- `gofmt` clean;
- `go mod tidy -diff` clean with the pinned Go patch version;
- `go vet ./...`;
- `go test -count=1 ./...`;
- `go test -race -count=1 ./...`;
- solver and report builds;
- immutable container-base checks and a network-isolated container build;
- standard-library-only dependency boundary;
- shadow-only source boundary;
- root-only, commit-pinned GitHub Actions workflow;
- pinned upstream contract record;
- loopback-only deployment policy.

A normal push run must validate the exact branch head and a normal pull-request
run must validate GitHub's current merge ref after this review record is added.

## Explicitly unsupported after Phase 0

- weighted-product pools;
- foreign limit-order liquidity;
- protocol-fee execution;
- pre/post interaction and wrapper execution;
- optimized partial-fill selection;
- cross-pair multi-order batching;
- authenticated competition-winner or objective-value evidence;
- signing, keys, RPC access and transaction submission.

These are roadmap work, not hidden capabilities of the recovery PR.

## Landing boundary

PR #2 must remain unmerged until its exact final head is green on both the
branch-head and merge-ref paths and Rohan explicitly authorizes that exact SHA.
Any later code change invalidates this review and requires fresh verification.
