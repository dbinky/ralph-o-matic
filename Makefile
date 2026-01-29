.PHONY: all build test test-unit test-integration test-coverage test-bats test-all clean

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Binary names
SERVER_BINARY=ralph-o-matic-server
CLI_BINARY=ralph-o-matic

# Directories
CMD_SERVER=./cmd/server
CMD_CLI=./cmd/cli
BUILD_DIR=./build

# Version injection
VERSION ?= $(shell git describe --tags --always --dirty)

# Build flags
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)" -trimpath

all: test build

## Build targets

build: build-cli

build-server:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY) $(CMD_SERVER)

build-cli:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY) $(CMD_CLI)

## Cross-compilation targets

build-all: build-cli-all

build-server-all:
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-darwin-arm64 $(CMD_SERVER)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-darwin-amd64 $(CMD_SERVER)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-linux-amd64 $(CMD_SERVER)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-linux-arm64 $(CMD_SERVER)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-windows-amd64.exe $(CMD_SERVER)

build-cli-all:
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY)-darwin-arm64 $(CMD_CLI)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY)-darwin-amd64 $(CMD_CLI)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY)-linux-amd64 $(CMD_CLI)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY)-linux-arm64 $(CMD_CLI)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY)-windows-amd64.exe $(CMD_CLI)

## Test targets

test: test-unit

test-unit:
	$(GOTEST) -v -short -race ./...

test-integration:
	$(GOTEST) -v -race -tags=integration ./...

test-coverage:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

test-bats:
	bats scripts/tests/

test-all: test-unit test-bats

## Utility targets

clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

deps:
	$(GOMOD) download
	$(GOMOD) verify
	$(GOMOD) tidy

lint:
	golangci-lint run ./...

fmt:
	$(GOCMD) fmt ./...

vet:
	$(GOCMD) vet ./...

## Skill packaging

.PHONY: package-skill
package-skill:
	@echo "Packaging brainstorm-to-ralph skill..."
	@mkdir -p dist
	@tar -czvf dist/brainstorm-to-ralph-skill.tar.gz -C skills brainstorm-to-ralph
	@cd skills && zip -r ../dist/brainstorm-to-ralph-skill.zip brainstorm-to-ralph
	@echo "Skill packaged: dist/brainstorm-to-ralph-skill.tar.gz"
	@echo "Skill packaged: dist/brainstorm-to-ralph-skill.zip"

.PHONY: release
release: build-all package-skill
	@echo "Release artifacts ready in dist/"
