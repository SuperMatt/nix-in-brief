.PHONY: install test clean

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

install:
	GOBIN=$(HOME)/.local/bin go install -ldflags "-X main.version=$(VERSION)" ./cmd/nib/

test:
	go test ./cmd/nib/ -v

clean:
	go clean -i ./cmd/nib/
