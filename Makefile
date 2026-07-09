BIN := bmc$(if $(filter windows,$(shell go env GOOS)),.exe)
GOBIN := $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)

.PHONY: build install test

build:
	go build -o $(BIN) .

install:
	go build -o "$(GOBIN)/$(BIN)" .

test:
	go vet ./...
	go test ./...
