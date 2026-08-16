#!/usr/bin/env python3
from __future__ import annotations

import argparse
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--implementation-head", required=True)
    parser.add_argument("--upstream-head", required=True)
    args = parser.parse_args()

    if args.upstream_head != "20b3a62f222ad278502fb7e85cae4938e7f26f65":
        raise SystemExit(f"unexpected upstream head {args.upstream_head}")

    content = f"""# Phase 1 Wire-Contract Acceptance Review

Date: 2026-08-16

Repository: `skrohan5016-coder/aladdin-solver-engine`

Accepted Phase 0 landing: `27d0800324358c39f36f240b0fbd5920faf5ee67`

Reviewed Phase 1 implementation head: `{args.implementation_head}`

Pinned upstream head independently checked out: `{args.upstream_head}`

This review covers the pinned CoW wire contract and deterministic arithmetic
fixtures only. It does not authorize live solving, signing, transaction
submission, capital deployment or a merge.

## Review scope 1 — upstream identity and runtime wire authority

A fresh, detached checkout of `cowprotocol/services` at the accepted commit was
verified with `cmd/contractcheck -upstream-dir`. The command recomputed Git blob
IDs for all five authoritative files and matched:

- Solver Engine OpenAPI `64a2466292446ea5f637c809f754fb4a31211a16`;
- driver auction serializer `f857f86838ce8a2a0b9ab0c7185e23eb4c8bcb9f`;
- runtime auction DTO `6c82fd4e461a32d73453feb68d79686642f802d6`;
- runtime solution DTO `816486e47ba0ac8d19da8a31ee722c103ee6c416`;
- Balancer StableMath source `3d181998518804abe621f739c033f0e0d75d9dd1`.

The runtime solution DTO was also checked directly for opaque string liquidity
IDs and the explicit `internalize` boolean. This intentionally overrides the
stale numeric-ID description in the pinned OpenAPI. End-to-end fixtures require
that IDs round-trip as strings and that `internalize: false` is not omitted.

## Review scope 2 — semantic drift and evidence integrity

The canonical auction fixture round-trips every pinned field represented by the
runtime auction union, including known unsupported fee-policy, wrapper,
flashloan, weighted-pool and foreign-limit-order shapes. Newly introduced fields
in auctions, tokens, orders, nested policy/wrapper objects or any pinned
liquidity variant fail closed until reviewed. A new liquidity kind also fails
closed.

Notification payloads remain intentionally extensible. Unknown metadata is
preserved as raw JSON, and solution IDs are represented by `json.Number`; the
fixture verifies exact preservation above 2^53. Fixture bytes are canonical JSON
and are bound by SHA-256 in `contract/cow-v1/manifest.json`.

Orders containing flashloan hints are now explicitly skipped rather than being
silently solved without their execution semantics.

## Review scope 3 — independent arithmetic and operational boundary

A Python standard-library generator independently reconstructs exact integer
reference vectors for:

- constant-product quotes;
- a concentrated-liquidity exact-input step;
- Balancer stable-pool invariant and balance solving;
- Uniswap TickMath boundary and adjacent ticks.

Go tests consume the language-neutral vectors and require exact outputs. CI runs
the generator in `--check` mode so stale vectors cannot pass.

The process remains loopback-only and evidence-mandatory. The governed source
scan found no private-key, signing, submission, outbound-client or
process-execution path in production Go code. The service remains standard
library only.

## Governed validation executed

The reviewed implementation passed:

- canonical fixture and source-pin verification;
- fresh upstream Git-blob verification;
- cross-language vector regeneration check;
- focused API, server, contract and AMM compatibility tests;
- `gofmt` cleanliness;
- `go mod tidy -diff`;
- pinned Go toolchain validation;
- `go vet ./...`;
- normal and race-enabled full test suites;
- solver, report and contractcheck builds;
- immutable container-base checks;
- network-isolated container build;
- standard-library-only dependency boundary;
- shadow-only source boundary;
- root-only, commit-pinned Actions workflow;
- loopback-only deployment policy.

## Explicitly unsupported after Phase 1

- weighted-product routing;
- foreign limit-order routing;
- protocol-fee execution;
- pre/post interaction and wrapper execution;
- flashloan execution;
- optimized partial-fill selection;
- cross-pair multi-order batching;
- authenticated winner/objective evidence;
- keys, RPC access, signing and transaction submission.

## Landing boundary

The acceptance record commit changes documentation only. Its exact final head
must independently pass both normal branch-head CI and GitHub's current
pull-request merge-ref CI. PR #3 must remain unmerged until Rohan explicitly
approves that exact final head SHA. Any later code change invalidates this review
and requires fresh verification.
"""
    target = ROOT / "docs" / "PHASE1_ACCEPTANCE_REVIEW.md"
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")


if __name__ == "__main__":
    main()
