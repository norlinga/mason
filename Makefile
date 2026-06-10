.PHONY: build test lint

build:
	go build -ldflags="-s -w" -o mason ./cmd/mason

test:
	go test ./...

lint:
	go vet ./...
