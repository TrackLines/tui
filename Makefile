VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build install test

build:
	go build -ldflags "$(LDFLAGS)" -o tracklines ./cmd/tracklines

install:
	go build -ldflags "$(LDFLAGS)" -o $(HOME)/.local/bin/tracklines ./cmd/tracklines

test:
	go test ./...
