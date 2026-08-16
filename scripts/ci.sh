#!/usr/bin/env bash
#
# Every gate this project enforces, in one place.
#
# Run it locally, from a git hook, or from GitHub Actions. Keeping the gates in
# this script prevents local and hosted validation from drifting apart.
#
#   bash scripts/ci.sh
#
set -uo pipefail

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

section "Static analysis"
run_gate "go vet" go vet ./...

section "Tests"
run_gate "go test" go test ./...
run_gate "go test -race" go test -race ./...

section "Build"
run_gate "solver and report build" go build ./cmd/solver ./cmd/report

section "Dependency boundary"
# This service parses auction payloads from an external driver. Every
# third-party module is another parser in that path, so there are none.
if [ -f go.sum ] && [ -s go.sum ]; then
  printf '%s\n' "go.sum is non-empty — a third-party dependency was added"
  fail "stdlib-only dependency boundary"
else
  pass "stdlib only, no third-party modules"
fi

section "Shadow boundary"
# The engine proposes settlements. It must never be able to sign or send one.
pattern='PrivateKey|privateKey|mnemonic|MNEMONIC|SignTx|SendTransaction|eth_sendRawTransaction'
hits="$(grep -rInE "$pattern" --include='*.go' . 2>/dev/null || true)"
if [ -z "$hits" ]; then
  pass "no key, signing, or submission paths"
else
  printf '%s\n' "$hits"
  fail "signing or submission path found"
fi

section "Workflow placement"
if [ -f .github/workflows/ci.yml ]; then
  pass "root GitHub Actions workflow present"
else
  fail "missing .github/workflows/ci.yml"
fi

section "Result"
if [ "$fails" -eq 0 ]; then
  printf '  \033[32mall gates passed\033[0m\n\n'
  exit 0
fi
printf '  \033[31m%d gate(s) failed\033[0m\n\n' "$fails"
exit 1
