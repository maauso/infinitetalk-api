SHELL := /bin/bash

GO ?= go
PKG := ./...

# Prefer local binaries; fallback to Docker images when missing
GOLANGCI_BIN := $(shell command -v golangci-lint 2>/dev/null)
GOLANGCI_CMD := $(if $(GOLANGCI_BIN),$(GOLANGCI_BIN) run, docker run --rm -v $(PWD):/app -w /app golangci/golangci-lint:latest golangci-lint run)

MOCKERY_BIN := $(shell command -v mockery 2>/dev/null)
MOCKERY_CMD := $(if $(MOCKERY_BIN),$(MOCKERY_BIN),docker run --rm -v "$(PWD)":/src -w /src vektra/mockery:v3.6.1)

.PHONY: build test lint linters lint-fix mocks tidy fmt

# Build the application binary
build:
	$(GO) build -o infinitetalk-api .

# Run tests with race detection
test:
	$(GO) test -race -count=1 $(PKG)

# Run linter
lint:
	$(GOLANGCI_CMD)

# Alias commonly used name
linters: lint

lint-fix:
	$(if $(GOLANGCI_BIN),$(GOLANGCI_BIN) run --fix, docker run --rm -v $(PWD):/app -w /app golangci/golangci-lint:latest golangci-lint run --fix)

mocks:
	$(MOCKERY_CMD)

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt $(PKG)
