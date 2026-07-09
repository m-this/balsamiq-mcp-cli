GO := $(shell command -v go 2>/dev/null)
BIN = bmc$(if $(filter windows,$(shell go env GOOS)),.exe)
GOBIN = $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)

.PHONY: build install test check-go

check-go:
	@test -n "$(GO)" || { echo "error: Go toolchain not found in PATH; install it from https://go.dev/dl/"; exit 1; }

build: check-go
	go build -o $(BIN) .

install: check-go
	go build -o "$(GOBIN)/$(BIN)" .

test: check-go
	go vet ./...
	go test ./...
