# MCP HTTP Bridge Makefile
# Build and development automation for the MCP HTTP bridge

# Project metadata
BINARY_NAME := mcp-bridge
MODULE := github.com/pgEdge/pgedge-mcp-bridge
VERSION_PKG := $(MODULE)/pkg/version

# Build directories
BIN_DIR := bin
COVERAGE_DIR := coverage

# Version information from git
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Build flags
LDFLAGS := -ldflags "-s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).BuildTime=$(BUILD_TIME) \
	-X $(VERSION_PKG).GitCommit=$(GIT_COMMIT)"

# Go commands
GO := go
GOTEST := $(GO) test
GOBUILD := $(GO) build
GOFMT := gofmt
GOVET := $(GO) vet

# Test flags
TEST_FLAGS := -race -v
COVERAGE_FLAGS := -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic

# Platforms for cross-compilation
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

# Coverage threshold (percentage)
COVERAGE_THRESHOLD := 90

.PHONY: all build build-all clean test test-coverage test-coverage-check \
        lint fmt fmt-check vet check deps install-tools help

# Default target
all: build

## build: Build binary to bin/mcp-bridge
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/mcp-bridge

## build-all: Build for linux/darwin/windows amd64/arm64
build-all:
	@echo "Building for all platforms..."
	@mkdir -p $(BIN_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*}; \
		GOARCH=$${platform#*/}; \
		output=$(BIN_DIR)/$(BINARY_NAME)-$${GOOS}-$${GOARCH}; \
		if [ "$${GOOS}" = "windows" ]; then \
			output=$${output}.exe; \
		fi; \
		echo "Building $${output}..."; \
		GOOS=$${GOOS} GOARCH=$${GOARCH} $(GOBUILD) $(LDFLAGS) -o $${output} ./cmd/mcp-bridge || exit 1; \
	done
	@echo "Build complete. Binaries are in $(BIN_DIR)/"

## clean: Remove bin/ and coverage/
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BIN_DIR)
	@rm -rf $(COVERAGE_DIR)
	@echo "Clean complete."

## test: Run tests with race detector
test:
	@echo "Running tests..."
	$(GOTEST) $(TEST_FLAGS) ./...

## test-coverage: Run tests with coverage, generate HTML report in coverage/
test-coverage:
	@echo "Running tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	$(GOTEST) $(TEST_FLAGS) $(COVERAGE_FLAGS) ./...
	@$(GO) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@$(GO) tool cover -func=$(COVERAGE_DIR)/coverage.out
	@echo "Coverage report generated: $(COVERAGE_DIR)/coverage.html"

## test-coverage-check: Check coverage meets 90% threshold
test-coverage-check: test-coverage
	@echo "Checking coverage threshold ($(COVERAGE_THRESHOLD)%)..."
	@coverage=$$($(GO) tool cover -func=$(COVERAGE_DIR)/coverage.out | grep total | awk '{print $$3}' | tr -d '%'); \
	if [ -z "$$coverage" ]; then \
		echo "Error: Could not determine coverage percentage"; \
		exit 1; \
	fi; \
	if [ $$(echo "$$coverage < $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
		echo "Coverage $$coverage% is below threshold of $(COVERAGE_THRESHOLD)%"; \
		exit 1; \
	else \
		echo "Coverage $$coverage% meets threshold of $(COVERAGE_THRESHOLD)%"; \
	fi

## lint: Run golangci-lint
lint:
	@echo "Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Run 'make install-tools' first."; \
		exit 1; \
	fi

## fmt: Format code with gofmt
fmt:
	@echo "Formatting code..."
	@$(GOFMT) -s -w .
	@echo "Formatting complete."

## fmt-check: Check formatting
fmt-check:
	@echo "Checking code formatting..."
	@unformatted=$$($(GOFMT) -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not formatted:"; \
		echo "$$unformatted"; \
		echo "Run 'make fmt' to fix."; \
		exit 1; \
	fi
	@echo "All files are properly formatted."

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

## check: Run fmt-check, vet, lint, test
check: fmt-check vet lint test
	@echo "All checks passed."

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy
	@echo "Dependencies downloaded."

## install-tools: Install golangci-lint
install-tools:
	@echo "Installing development tools..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin; \
	else \
		echo "golangci-lint is already installed."; \
	fi
	@echo "Tools installation complete."

## help: Show available targets
help:
	@echo "MCP HTTP Bridge - Available Targets"
	@echo "===================================="
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | awk -F: '{printf "  %-25s %s\n", $$1, $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  make build          # Build the binary"
	@echo "  make test           # Run tests"
	@echo "  make check          # Run all checks before commit"
	@echo "  make build-all      # Build for all platforms"
