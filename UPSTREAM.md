# Upstream Contract Pin

The shadow engine is reviewed against an exact snapshot of the CoW Protocol
reference implementation. This pin is a source-review contract; the running
service performs no network fetch and does not depend on the upstream repository
at runtime.

## Accepted upstream snapshot

- Repository: `cowprotocol/services`
- Commit: `20b3a62f222ad278502fb7e85cae4938e7f26f65`
- Commit date: 2026-08-14
- Pin reviewed: 2026-08-16

Authoritative files used by this repository:

| Purpose | Upstream path | Git blob SHA |
|---|---|---|
| Driver auction serialization | `crates/driver/src/infra/solver/dto/auction.rs` | `f857f86838ce8a2a0b9ab0c7185e23eb4c8bcb9f` |
| Runtime auction DTO | `crates/solvers-dto/src/auction.rs` | `6c82fd4e461a32d73453feb68d79686642f802d6` |
| Runtime solution DTO | `crates/solvers-dto/src/solution.rs` | `816486e47ba0ac8d19da8a31ee722c103ee6c416` |
| Solver HTTP and documented wire schema | `crates/solvers/openapi.yml` | `64a2466292446ea5f637c809f754fb4a31211a16` |
| Balancer stable-pool arithmetic | `crates/liquidity-sources/src/balancer_v2/swap/stable_math.rs` | `3d181998518804abe621f739c033f0e0d75d9dd1` |

The stable-math source defines `AMP_PRECISION = 1000`; the driver serializes
amplification and scaling values as decimals. The Go parser converts those
wire decimals back to the fixed-point integers consumed by the arithmetic.

Runtime DTOs are authoritative when OpenAPI and implementation disagree. At this
pin, `LiquidityInteraction.id` is a string in the runtime solution DTO even
though OpenAPI describes a number, and `internalize` is an explicit boolean. The
engine follows the runtime DTO and records the discrepancy in `UPSTREAM_PIN.json`.

## Contract rules

The engine currently consumes:

- auction identity, deadline, gas price, tokens, orders and supplied liquidity;
- constant-product, concentrated-liquidity and stable-pool fields;
- order execution semantics needed to decide whether an order is supported;
- extensible notification payloads, preserving unknown metadata verbatim.

The engine emits only fulfillment trades and supplied-liquidity interactions.
It fails closed when an interaction liquidity ID cannot be represented by the
current upstream numeric wire field.

Fields whose execution semantics are not implemented are not ignored. Orders
with fee policies, wrappers, hooks, unsupported balance modes or unknown kinds
are counted and skipped.

## Updating the pin

An upstream update requires a dedicated pull request that:

1. records the new exact commit and blob SHAs;
2. reviews the OpenAPI and driver DTO diffs;
3. updates representative fixtures and compatibility tests;
4. revalidates stable arithmetic when the math source changes;
5. runs the complete exact-head and pull-request CI gates;
6. documents any newly unsupported field before landing.

Do not silently move this pin as part of an unrelated solver optimization.
