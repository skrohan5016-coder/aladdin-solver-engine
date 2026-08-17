# Phase 1 — Continuously Verified CoW Wire Contract

This phase binds the shadow engine to an exact `cowprotocol/services` snapshot
and makes wire incompatibility a failing test rather than a production surprise.
It does not add signing, RPC access, transaction submission or live capital.

## Accepted upstream authority

- repository: `cowprotocol/services`
- commit: `20b3a62f222ad278502fb7e85cae4938e7f26f65`
- machine-readable pin: `UPSTREAM_PIN.json`

The runtime Rust DTOs are authoritative for actual serialization. The driver DTO
shows how auctions are constructed. OpenAPI remains useful documentation, but it
is not allowed to override runtime DTO behavior when they disagree.

Three reviewed discrepancies matter immediately:

1. OpenAPI describes `Solution.interactions[].id` as a number, while the runtime
   solution DTO deserializes it as a string. The engine now preserves and emits
   the auction's opaque string ID.
2. The runtime solution DTO requires `internalize: bool`. The engine now emits an
   explicit boolean even when the value is false.
3. The runtime notification DTO makes `auctionId` and `solutionId` optional and
   defines `solutionId` as one `u64` or a merged `u64[]`. Both forms are
   preserved without floating-point conversion.

## Retained fixtures

`testdata/contracts` contains representative pinned examples:

- a direct constant-product auction and its exact normalized solution;
- an auction covering every upstream liquidity variant and settlement-semantic
  order field, including fees, wrappers and a flash-loan hint;
- an extensible driver notification with nested unknown metadata and the
  variant-specific fields required by the pinned runtime DTO.

`manifest.json` binds every fixture byte sequence by SHA-256 and to the accepted
upstream commit. `cmd/contractcheck` validates the fixtures, replays the direct
auction, and proves notification metadata survives a decode/encode round trip.

## Semantic fail-closed policy

Auction objects, orders, supported liquidity variants and emitted solutions use
strict field allow-lists. A newly introduced field is rejected until reviewed if
silently ignoring it could change settlement execution or scoring. Auction IDs,
token decimals, deadlines, order amounts and `validTo` values are also bounded by
the scalar types of the pinned runtime DTO.

Notification objects remain extensible because upstream may add non-semantic
metadata. Their known `kind` variants and variant-specific required fields are
validated first; unknown extra fields are then preserved losslessly.

Orders carrying unsupported execution authority remain excluded from solving:

- fee policies;
- wrappers;
- pre/post interactions;
- non-ERC20 balance modes;
- flash-loan hints.

The last item is explicit in Phase 1: a flash-loan hint is not metadata. Ignoring
it could change the settlement lifecycle, so the engine skips that order until a
separate reviewed implementation exists.

## Independent arithmetic vectors

`scripts/generate_reference_vectors.py` is a standard-library Python
implementation independent of the Go quote code. It deterministically regenerates
`testdata/reference/pool-vectors-v1.json` for:

- constant-product `getAmountOut`;
- Uniswap V3 TickMath and a single-range exact-input quote;
- the pinned Balancer V2 stable invariant and output calculation.

Go tests consume those generated vectors. CI fails when the generator output and
committed vectors differ.

## Drift and pin governance

The normal CI path is fully offline and validates the accepted pin, fixtures,
replay and arithmetic vectors. A separate weekly/manual workflow compares the
six authoritative files on upstream `main` against the accepted blobs and fails
when they drift.

Changing the accepted pin requires a dedicated pull request with:

- updated `UPSTREAM_PIN.json` and `UPSTREAM.md`;
- updated fixture manifest;
- a review record at `docs/upstream-pin-reviews/<new-commit>.md`;
- all exact-head and merge-ref gates.

## Acceptance boundary

Phase 1 is accepted only when the exact final PR head and GitHub merge ref both
pass formatting, vet, tests, race tests, builds, contract fixture validation,
independent vector validation, dependency/shadow boundaries and pin governance.
A later code change invalidates prior evidence and requires fresh verification.
