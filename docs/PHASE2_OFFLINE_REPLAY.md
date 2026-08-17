# Phase 2 — Reproducible Offline Replay Corpus

Phase 2 converts opt-in full-auction recorder output into a sealed, private,
offline corpus whose input, expected output, source identity, toolchain identity,
upstream contract, and complete solver configuration are all explicit.

It does not add RPC access, signing, transaction submission, capital, or any
other live-execution authority.

## Record identity

When `RECORD_FULL_AUCTIONS=true`, every auction record uses
`aladdin-shadow-auction-record/v2` and contains:

- the complete auction payload;
- the exact compiled engine commit;
- Go version, operating system, and architecture;
- the pinned CoW upstream commit;
- a versioned snapshot of every solver configuration knob;
- SHA-256 of that configuration snapshot;
- recorded solutions and diagnostic statistics.

Full recording refuses to start when the binary does not contain an exact
40-hex source commit. `make build` embeds the current Git commit. Container
builds must pass `--build-arg ENGINE_COMMIT=<exact-sha>` before enabling full
recording.

## Corpus publication

Create a new corpus from one or more daily recorder files:

```sh
bin/replay pack \
  -records '/opt/solver/data/auctions-2026-08-*.jsonl' \
  -out /opt/solver/corpora/august \
  -source-commit <exact-engine-commit>
```

Publication is fail-closed:

- every JSONL record must end in a newline;
- every record line is size-bounded and strictly decoded;
- all records must share one source, toolchain, upstream, and config identity;
- recorded solutions and statistics must reproduce before publication;
- the destination must not already exist;
- files are written into a private temporary directory with exclusive creation;
- `manifest.json` is written last and the completed directory is atomically
  renamed into place;
- symlinked parents, symlinked inputs, duplicate inventory, partial records, and
  corruption are rejected.

## Redaction and retention

The default `signatures` redaction replaces order signatures with `0x` only
after proving that this does not change solver output. `none` retains exact
auction bytes. UIDs, owners, receivers, app data, amounts, and liquidity remain
because changing them can alter matching, routing, or evidence identity.

Corpora therefore remain sensitive private evidence. Keep them under the solver
service account, mode `0700` for directories and `0600` for files. Do not commit
production corpora to this repository. Retention and deletion are operational
choices, but a report must never cite a corpus after any file or its manifest
has been removed or modified.

## Replay verification

Verify a sealed corpus without network access:

```sh
bin/replay verify \
  -dir /opt/solver/corpora/august \
  -source-commit <exact-engine-commit>
```

The verifier checks the exact inventory, byte lengths, SHA-256 values, schemas,
source commit, pinned upstream commit, Go version, and config digest. It then
replays every auction and compares canonical expected solutions and statistics.
The emitted replay report contains deterministic corpus and result digests; two
runs over the same accepted corpus produce identical report bytes.

Unknown files, missing files, symlinks, oversized files, malformed JSON,
duplicate keys, source/config/toolchain mismatch, or a changed replay result all
produce a non-zero exit.
