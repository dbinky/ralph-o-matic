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

build: build-server build-cli

build-server:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY) $(CMD_SERVER)

build-cli:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLI_BINARY) $(CMD_CLI)

## Cross-compilation targets

build-all: build-server-all build-cli-all

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

## Plugin & skill packaging

.PHONY: plugin-validate
plugin-validate:
	@echo "Validating plugin structure..."
	@test -f .claude-plugin/plugin.json || (echo "Missing .claude-plugin/plugin.json" && exit 1)
	@test -f .claude-plugin/marketplace.json || (echo "Missing .claude-plugin/marketplace.json" && exit 1)
	@for skill in skills/*/; do \
		test -f "$$skill/SKILL.md" || (echo "Missing SKILL.md in $$skill" && exit 1); \
	done
	@echo "Plugin structure valid"

.PHONY: package-skills
package-skills: plugin-validate
	@mkdir -p dist
	@for skill in skills/*/; do \
		name=$$(basename "$$skill"); \
		echo "Packaging $$name skill..."; \
		tar -czvf "dist/$$name-skill.tar.gz" -C skills "$$name"; \
		cd skills && zip -r "../dist/$$name-skill.zip" "$$name" && cd ..; \
		echo "Skill packaged: dist/$$name-skill.tar.gz"; \
		echo "Skill packaged: dist/$$name-skill.zip"; \
	done

.PHONY: release
release: build-all package-skills
	@echo "Release artifacts ready in dist/"
