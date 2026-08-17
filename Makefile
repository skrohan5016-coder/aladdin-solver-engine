.PHONY: build test race lint contract ci hooks run report pack-corpus replay-corpus clean

COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || printf unknown)
LDFLAGS = -s -w -X github.com/skrohan5016-coder/aladdin-solver-engine/internal/buildinfo.Commit=$(COMMIT)

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/solver ./cmd/solver
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/report ./cmd/report
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/replay ./cmd/replay
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/contractcheck ./cmd/contractcheck

test:
	go test ./...

race:
	go test -race ./...

lint:
	gofmt -l .
	go vet ./...

contract:
	go run ./cmd/contractcheck -dir testdata/contracts
	python3 scripts/generate_reference_vectors.py --check

# Every gate, exactly as CI runs them.
ci:
	bash scripts/ci.sh

# Run the gates automatically before each push.
hooks:
	bash scripts/install-hooks.sh

run: build
	./bin/solver

report: build
	./bin/report -dir ./data

# Usage: make pack-corpus RECORDS='./data/auctions-*.jsonl' CORPUS=./private-corpus
pack-corpus: build
	test -n "$(RECORDS)"
	test -n "$(CORPUS)"
	./bin/replay pack -records "$(RECORDS)" -out "$(CORPUS)"

# Usage: make replay-corpus CORPUS=./private-corpus
replay-corpus: build
	test -n "$(CORPUS)"
	./bin/replay verify -dir "$(CORPUS)"

clean:
	rm -rf bin/
