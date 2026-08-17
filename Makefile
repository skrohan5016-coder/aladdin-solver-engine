.PHONY: build test race lint contract ci hooks run report clean

build:
	go build -trimpath -ldflags="-s -w" -o bin/solver ./cmd/solver
	go build -trimpath -ldflags="-s -w" -o bin/report ./cmd/report
	go build -trimpath -ldflags="-s -w" -o bin/contractcheck ./cmd/contractcheck

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

clean:
	rm -rf bin/
