#!/usr/bin/env bash
# Every gate this project enforces, shared by local development and Actions.
set -uo pipefail
export GOTOOLCHAIN=local

cd "$(dirname "$0")/.." || exit 1

fails=0
pass() { printf '  \033[32mPASS\033[0m  %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m  %s\n' "$1"; fails=$((fails + 1)); }
section() { printf '\n\033[1m%s\033[0m\n' "$1"; }

run_gate() {
  local label="$1"
  shift
  local output
  if output="$("$@" 2>&1)"; then
    if [ -n "$output" ]; then
      printf '%s\n' "$output"
    fi
    pass "$label"
  else
    if [ -n "$output" ]; then
      printf '%s\n' "$output"
    fi
    fail "$label"
  fi
}

section "Formatting"
unformatted="$(gofmt -l . 2>/dev/null)"
if [ -z "$unformatted" ]; then
  pass "gofmt clean"
else
  printf '%s\n' "$unformatted"
  fail "gofmt would rewrite files"
fi

section "Module contract"
run_gate "go.mod is tidy" go mod tidy -diff
modules="$(go list -m all 2>/dev/null || true)"
module_count="$(printf '%s\n' "$modules" | sed '/^$/d' | wc -l | tr -d ' ')"
if [ "$module_count" = "1" ]; then
  pass "only the repository module is present"
else
  printf '%s\n' "$modules"
  fail "third-party Go modules are present"
fi
if grep -qx 'go 1.24.13' go.mod; then
  pass "Go patch version is pinned"
else
  fail "go.mod must pin go 1.24.13"
fi

section "Pinned wire contract"
run_gate "fixture byte digests" python3 scripts/check_contract_fixtures.py
run_gate "upstream pin consistency" python3 scripts/check_upstream_pin.py
run_gate "independent arithmetic vectors" python3 scripts/generate_reference_vectors.py --check
run_gate "contract fixtures and replay" go run ./cmd/contractcheck -dir testdata/contracts
run_gate "pin-change governance" python3 scripts/check_pin_change.py
run_gate "workflow action pins" python3 scripts/check_workflow_pins.py
run_gate "Python helper syntax" python3 -m py_compile scripts/check_contract_fixtures.py scripts/check_pin_change.py scripts/check_upstream_drift.py scripts/check_upstream_pin.py scripts/check_workflow_pins.py scripts/generate_reference_vectors.py

section "Static analysis"
run_gate "go vet" go vet ./...

section "Tests"
run_gate "go test" go test -count=1 ./...
run_gate "go test -race" go test -race -count=1 ./...

section "Build"
run_gate "solver, report and contractcheck build" go build -trimpath ./cmd/solver ./cmd/report ./cmd/contractcheck
if grep -q '^FROM golang:1.24.13-alpine3.22@sha256:3641e0d9b931dc4f2f185dcd669c4679670e9277c8166a838ddb98a2d4389cb5 AS build$' Dockerfile &&
  grep -q '^FROM scratch$' Dockerfile; then
  pass "container bases are immutable"
else
  fail "Dockerfile must use the reviewed Go digest and scratch runtime"
fi
if command -v docker >/dev/null 2>&1; then
  run_gate "network-isolated container build" docker build --network=none -t aladdin-solver-engine:ci .
else
  pass "Docker unavailable; immutable Dockerfile contract checked statically"
fi

section "Dependency boundary"
if [ -f go.sum ] && [ -s go.sum ]; then
  printf '%s\n' "go.sum is non-empty — a third-party dependency was added"
  fail "stdlib-only dependency boundary"
else
  pass "stdlib only, no third-party modules"
fi

section "Shadow boundary"
secret_pattern='PrivateKey|privateKey|mnemonic|MNEMONIC|SignTx|SendTransaction|eth_sendRawTransaction'
secret_hits="$(grep -rInE "$secret_pattern" --include='*.go' --exclude='*_test.go' . 2>/dev/null || true)"
if [ -z "$secret_hits" ]; then
  pass "no key, signing, or submission paths"
else
  printf '%s\n' "$secret_hits"
  fail "signing or submission path found"
fi

outbound_pattern='http\.(Client|DefaultClient|Transport|NewRequest|Get|Post|PostForm)|net\.Dial|DialContext|tls\.Dial|rpc\.Dial|exec\.Command|"os/exec"|ethclient|websocket'
outbound_hits="$(grep -rInE "$outbound_pattern" --include='*.go' --exclude='*_test.go' . 2>/dev/null || true)"
if [ -z "$outbound_hits" ]; then
  pass "no outbound client or process-execution path"
else
  printf '%s\n' "$outbound_hits"
  fail "outbound client or process-execution path found"
fi

section "Workflow and pin placement"
misplaced="$(find . -type f -path '*/.github/workflows/*' ! -path './.github/workflows/*' -print)"
if [ -z "$misplaced" ] && [ -f .github/workflows/ci.yml ]; then
  pass "only root GitHub Actions workflows are active"
else
  printf '%s\n' "$misplaced"
  fail "workflow is missing or misplaced"
fi
if grep -q 'actions/checkout@fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09' .github/workflows/ci.yml &&
  grep -q 'actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16' .github/workflows/ci.yml; then
  pass "GitHub Actions are pinned by commit"
else
  fail "GitHub Actions must be pinned by reviewed commit"
fi
if grep -q '20b3a62f222ad278502fb7e85cae4938e7f26f65' UPSTREAM.md; then
  pass "upstream solver contract is pinned"
else
  fail "missing upstream solver-contract pin"
fi

section "Repository cleanliness"
temporary_paths=(
  ".phase1-review-trigger"
  "scripts/.phase1_review_fix.py.gz"
  "scripts/phase1_acceptance.py"
  "scripts/phase1_builder.py"
)
temporary_hits=()
for path in "${temporary_paths[@]}"; do
  if [ -e "$path" ]; then
    temporary_hits+=("$path")
  fi
done
while IFS= read -r path; do
  temporary_hits+=("$path")
done < <(find .github/workflows -maxdepth 1 -type f -name 'phase1-*' -print | sort)
if [ "${#temporary_hits[@]}" -eq 0 ]; then
  pass "no temporary Phase 1 automation or payloads"
else
  printf '%s\n' "${temporary_hits[@]}"
  fail "temporary Phase 1 automation or payloads remain"
fi

section "Deployment boundary"
if grep -q 'LISTEN_ADDR=127.0.0.1:8000' deploy/solver.service &&
  grep -q 'IPAddressDeny=any' deploy/solver.service; then
  pass "deployment is loopback-only with external egress denied"
else
  fail "deployment network boundary is not enforced"
fi

section "Result"
if [ "$fails" -eq 0 ]; then
  printf '  \033[32mall gates passed\033[0m\n\n'
  exit 0
fi
printf '  \033[31m%d gate(s) failed\033[0m\n\n' "$fails"
exit 1
