# Phase 1 Wire-Contract Acceptance Review

Date: 2026-08-16

Repository: `skrohan5016-coder/aladdin-solver-engine`

Accepted Phase 0 main: `27d0800324358c39f36f240b0fbd5920faf5ee67`

Review baseline before closure: `3e51a0dbdcbf12159f10829f0b2c24c24bd66e3c`

Pinned upstream: `cowprotocol/services@20b3a62f222ad278502fb7e85cae4938e7f26f65`

The exact final candidate SHA and its branch-head and merge-ref run identities are
published in PR #3 after GitHub creates and verifies the final commit. This file
records the review scope and remediation; it does not authorize a merge or live
execution.

## Correctness and runtime compatibility

The accepted runtime Rust DTOs, rather than OpenAPI where they differ, remain the
serialization authority. The review verified the six pinned source blobs covering
the driver auction serializer, runtime auction, solution and notification DTOs,
OpenAPI, and Balancer stable arithmetic.

The final closure adds fail-closed scalar validation for runtime auction fields
that were previously represented by wider Go types:

- optional auction IDs must fit the upstream `i64` display-string contract;
- token decimals must fit `u8`;
- gas prices, balances, reference prices, and all four order amounts must be
  bounded decimal `uint256` strings;
- deadlines must be RFC 3339 timestamps;
- `validTo` must fit `u32`;
- required full-order amounts may not silently decode as empty strings.

Notification IDs remain optional and lossless: `solutionId` accepts either one
`u64` or a merged `u64[]`, including values above the exact floating-point range.
The known runtime `kind` variants now enforce their variant-specific fields,
including 32-byte transaction hashes, addresses, `uint256` balances, reasons,
and simulation transaction structure. Unknown non-semantic metadata is retained
and round-tripped unchanged.

## Determinism and retained evidence

The reviewed corpus contains:

- `testdata/contracts/auction-direct.json`;
- `testdata/contracts/auction-all-liquidity.json`;
- `testdata/contracts/solution-direct.json`;
- `testdata/contracts/notification-extra.json`;
- `testdata/contracts/manifest.json`;
- `testdata/reference/pool-vectors-v1.json`.

Every retained contract fixture is SHA-256 bound to the accepted upstream commit.
`cmd/contractcheck` validates the manifest, rejects unsafe fixture paths and
unlisted JSON files, checks the bytes, replays the direct auction, normalizes the
solution, and proves notification round-trip preservation. Independent Python
vectors remain the authority cross-check for constant-product, Uniswap V3, and
stable-pool arithmetic.

## Adversarial and security review

The final candidate preserves or strengthens the following boundaries:

- duplicate JSON keys and trailing top-level values fail closed;
- unknown settlement-semantic auction and solution fields fail closed;
- unsupported fee policies, wrappers, hooks, balance modes, flash-loan semantics,
  weighted execution, and foreign limit-order execution are not silently used;
- fixture traversal, symlink, manifest-collision, and digest mismatches are
  rejected;
- the engine contains no private-key, signing, transaction-submission, outbound
  RPC/client, or process-execution path;
- deployment remains loopback-only with external egress denied;
- notification extension fields cannot replace or smuggle core IDs or `kind`;
- exact integer representations are never routed through floating point.

## CI and reproducibility review

Normal CI and the weekly/manual upstream-drift workflow are the only permanent
GitHub Actions workflows allowed in the final tree. The temporary Phase 1
finalizer is deleted, and the shared CI script now rejects any future temporary
or ungoverned workflow instead of relying on a name pattern.

The authoritative final gates are the pinned GitHub runs on the exact final head
and current merge ref. They include formatting, module tidiness, fixture and pin
checks, independent-vector regeneration, `go vet`, normal and race tests, command
builds, immutable container bases, standard-library-only dependencies, shadow
boundaries, workflow pinning and placement, repository cleanliness, and the
deployment network boundary.

## Remaining explicit limitations

Phase 1 does not implement weighted-product execution, foreign limit-order
execution, protocol-fee execution, hooks or wrappers, optimized partial fills,
cross-pair batching, flash-loan execution, authenticated winner/objective
evidence, keys, RPC access, signing, submission, or capital deployment.

## Landing boundary

The final tree must contain no temporary finalization workflow or payload. PR #3
must remain unmerged until both exact-head CI paths are successful, no current
review thread remains unresolved, and Rohan explicitly approves that exact final
SHA for squash merge. Any later commit invalidates the evidence.
