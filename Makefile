# Makefile for kube-packet-replay

# Variables
BINARY_NAME=kube-packet-replay
MAIN_PATH=./main.go
BUILD_DIR=./build
DIST_DIR=./dist
GO=go
GOFLAGS=-v
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Go build flags
LDFLAGS=-ldflags "-w -s -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"
KIND_CLUSTER_NAME=kpr-e2e

# Detect OS and Architecture
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

ifeq ($(UNAME_S),Linux)
	OS=linux
endif
ifeq ($(UNAME_S),Darwin)
	OS=darwin
endif
ifeq ($(UNAME_M),x86_64)
	ARCH=amd64
endif
ifeq ($(UNAME_M),arm64)
	ARCH=arm64
endif

# Default target
.DEFAULT_GOAL := help

# Help target
.PHONY: help
help: ## Show this help message
	@echo "kube-packet-replay - Capture and replay network packets in Kubernetes"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*##"; printf ""} /^[a-zA-Z_-]+:.*?##/ { printf "  %-20s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# Build targets
.PHONY: build
build: ## Build the binary for current platform
	@echo "Building $(BINARY_NAME) for $(OS)/$(ARCH)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Binary created at $(BUILD_DIR)/$(BINARY_NAME)"

.PHONY: build-all
build-all: build-linux build-darwin build-windows ## Build for all supported platforms

.PHONY: build-linux
build-linux: ## Build for Linux (amd64 and arm64)
	@echo "Building for Linux..."
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 $(MAIN_PATH)

.PHONY: build-darwin
build-darwin: ## Build for macOS (amd64 and arm64)
	@echo "Building for macOS..."
	@mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)

.PHONY: build-windows
build-windows: ## Build for Windows (amd64)
	@echo "Building for Windows..."
	@mkdir -p $(DIST_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)

# Development targets
.PHONY: run
run: ## Run the application
	$(GO) run $(MAIN_PATH)

.PHONY: install
install: build ## Build and install the binary to GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	$(GO) install $(LDFLAGS) $(MAIN_PATH)

# Testing targets
.PHONY: test
test: ## Run all tests
	@echo "Running tests..."
	$(GO) test -v ./...

.PHONY: test-coverage
test-coverage: ## Run tests with coverage report
	@echo "Running tests with coverage..."
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated at coverage.html"

.PHONY: test-race
test-race: ## Run tests with race detector
	@echo "Running tests with race detector..."
	$(GO) test -race -v ./...

# Code quality targets
.PHONY: fmt
fmt: ## Format code using gofmt
	@echo "Formatting code..."
	$(GO) fmt ./...

.PHONY: lint
lint: ## Run golangci-lint
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install it with:"; \
		echo "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin"; \
	fi

.PHONY: vet
vet: ## Run go vet
	@echo "Running go vet..."
	$(GO) vet ./...

.PHONY: check
check: fmt vet lint test ## Run all checks (fmt, vet, lint, test)

# Dependency management
.PHONY: deps
deps: ## Download dependencies
	@echo "Downloading dependencies..."
	$(GO) mod download

.PHONY: deps-update
deps-update: ## Update dependencies
	@echo "Updating dependencies..."
	$(GO) get -u ./...
	$(GO) mod tidy

.PHONY: deps-verify
deps-verify: ## Verify dependencies
	@echo "Verifying dependencies..."
	$(GO) mod verify

# Clean targets
.PHONY: clean
clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR) $(DIST_DIR)
	@rm -f coverage.out coverage.html

.PHONY: clean-all
clean-all: clean ## Clean everything including go cache
	@echo "Cleaning go cache..."
	$(GO) clean -cache -testcache -modcache

# Docker targets
.PHONY: docker-build
docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):$(VERSION) .

.PHONY: docker-push
docker-push: ## Push Docker image to registry
	@echo "Pushing Docker image..."
	docker push $(BINARY_NAME):$(VERSION)

# Release targets
.PHONY: release
release: clean build-all ## Create release artifacts
	@echo "Creating release artifacts..."
	@mkdir -p $(DIST_DIR)/release
	@for file in $(DIST_DIR)/*; do \
		if [ -f $$file ]; then \
			basename=$$(basename $$file); \
			tar -czf $(DIST_DIR)/release/$$basename.tar.gz -C $(DIST_DIR) $$basename; \
			echo "Created $(DIST_DIR)/release/$$basename.tar.gz"; \
		fi \
	done
	@echo "Release artifacts created in $(DIST_DIR)/release/"

# Version information
.PHONY: version
version: ## Show version information
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "Go Version: $(shell $(GO) version)"

# Development helpers
.PHONY: dev-setup
dev-setup: ## Setup development environment
	@echo "Setting up development environment..."
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install golang.org/x/tools/cmd/goimports@latest
	@echo "Development tools installed"

.PHONY: gen
gen: ## Generate code (if any generators are used)
	@echo "Generating code..."
	$(GO) generate ./...

# Quick development cycle
.PHONY: dev
dev: fmt vet build ## Format, vet, and build for development

# KIND cluster management
.PHONY: kind-create
kind-create: ## Create a KIND cluster for e2e testing
	@echo "Creating KIND cluster '$(KIND_CLUSTER_NAME)'..."
	@kind create cluster --name $(KIND_CLUSTER_NAME) --config e2e/testdata/kind-config.yaml --wait 120s
	@echo "Pre-loading images into KIND..."
	@docker pull nicolaka/netshoot:v0.13 2>/dev/null || true
	@kind load docker-image nicolaka/netshoot:v0.13 --name $(KIND_CLUSTER_NAME)
	@docker pull nginx:1.25-alpine 2>/dev/null || true
	@kind load docker-image nginx:1.25-alpine --name $(KIND_CLUSTER_NAME)
	@docker pull curlimages/curl:8.5.0 2>/dev/null || true
	@kind load docker-image curlimages/curl:8.5.0 --name $(KIND_CLUSTER_NAME)
	@echo "KIND cluster '$(KIND_CLUSTER_NAME)' is ready"

.PHONY: kind-delete
kind-delete: ## Delete the KIND cluster
	@echo "Deleting KIND cluster '$(KIND_CLUSTER_NAME)'..."
	@kind delete cluster --name $(KIND_CLUSTER_NAME)

.PHONY: test-e2e
test-e2e: build ## Run e2e tests (requires KIND cluster)
	@echo "Running e2e tests..."
	@kind get kubeconfig --name $(KIND_CLUSTER_NAME) > /tmp/kpr-e2e-kubeconfig 2>/dev/null || true
	KUBECONFIG=/tmp/kpr-e2e-kubeconfig KPR_BINARY=$(CURDIR)/$(BUILD_DIR)/$(BINARY_NAME) $(GO) test -v -tags=e2e -timeout 10m -count=1 ./e2e/

# Watch for changes and rebuild
.PHONY: watch
watch: ## Watch for changes and rebuild (requires entr)
	@if command -v entr >/dev/null 2>&1; then \
		find . -name '*.go' | entr -r make build; \
	else \
		echo "entr not installed. Install it with your package manager."; \
		echo "  macOS: brew install entr"; \
		echo "  Linux: apt-get install entr or yum install entr"; \
	fi