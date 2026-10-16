.PHONY: build install test lint

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/norlinga/mason/internal/cli.version=$(VERSION)

build:
	go build -ldflags="$(LDFLAGS)" -o mason ./cmd/mason

# Install to /usr/local/bin (override with PREFIX=…); uses sudo only if needed.
PREFIX ?= /usr/local
install: build
	install -m 755 mason $(PREFIX)/bin/mason 2>/dev/null || sudo install -m 755 mason $(PREFIX)/bin/mason

test:
	go test ./...

lint:
	go vet ./...
