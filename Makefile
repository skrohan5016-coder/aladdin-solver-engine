.PHONY: build test race lint run report clean

build:
	go build -trimpath -ldflags="-s -w" -o bin/solver ./cmd/solver
	go build -trimpath -ldflags="-s -w" -o bin/report ./cmd/report

test:
	go test ./...

race:
	go test -race ./...

lint:
	gofmt -l .
	go vet ./...

run: build
	./bin/solver

report: build
	./bin/report -dir ./data

clean:
	rm -rf bin/
