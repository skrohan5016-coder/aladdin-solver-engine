#!/usr/bin/env bash
#
# Every gate this project enforces, in one place.
#
# Run it locally, from a git hook, or from GitHub Actions — the checks are
# identical in all three, so a green run on your machine means a green run in
# CI. Keeping them here rather than inline in the workflow YAML is also what
# makes the workflow file trivial enough to paste by hand.
#
#   bash scripts/ci.sh
#
set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

fails=0
pass() { printf '  \033[32mPASS\033[0m  %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m  %s\n' "$1"; fails=$((fails + 1)); }
head() { printf '\n\033[1m%s\033[0m\n' "$1"; }

head "Formatting"
unformatted="$(gofmt -l . 2>/dev/null)"
if [ -z "$unformatted" ]; then
  pass "gofmt clean"
else
  fail "gofmt would rewrite these files:"
  printf '        %s\n' $unformatted
  echo "        fix with: gofmt -w ."
fi

head "Static analysis"
if go vet ./... 2>&1; then
  pass "go vet"
else
  fail "go vet reported problems"
fi

head "Tests"
if go test -race ./... 2>&1 | grep -Ev '^\?' ; then
  pass "go test -race"
else
  fail "tests failed"
fi

head "Dependency boundary"
# This service parses auction payloads from an external driver. Every
# third-party module is another parser in that path, so there are none.
if [ -f go.sum ] && [ -s go.sum ]; then
  fail "go.sum is non-empty — a third-party dependency was added"
  echo "        this engine is stdlib-only by design"
else
  pass "stdlib only, no third-party modules"
fi

head "Shadow boundary"
# The engine proposes settlements. It must never be able to sign or send one.
pattern='PrivateKey|privateKey|mnemonic|MNEMONIC|SignTx|SendTransaction|eth_sendRawTransaction'
hits="$(grep -rInE "$pattern" --include='*.go' . 2>/dev/null)"
if [ -z "$hits" ]; then
  pass "no key, signing, or submission paths"
else
  fail "signing or submission path found:"
  printf '        %s\n' "$hits"
  echo "        if a live solver is genuinely needed, it belongs in a separate"
  echo "        repository with its own review standard — not in this one"
fi

head "Result"
if [ "$fails" -eq 0 ]; then
  printf '  \033[32mall gates passed\033[0m\n\n'
  exit 0
fi
printf '  \033[31m%d gate(s) failed\033[0m\n\n' "$fails"
exit 1
