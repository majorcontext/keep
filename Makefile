.PHONY: all help build build-cli build-relay build-gateway test test-unit test-integration lint lint-yaml fix fix-go fix-yaml clean coverage snapshot

# Default target - running "make" shows help
all: help

help: ## Show this help message
	@echo "Available targets:"
	@echo ""
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  make test                    # Run all tests"
	@echo "  make test-unit               # Run only unit tests"
	@echo "  make test-unit ARGS='-run TestName'           # Run specific unit test"
	@echo "  make test-unit ARGS='-run TestName ./internal/engine'  # Run test in specific package"

build: ## Build the project
	go build ./...

build-cli: ## Build the keep CLI binary
	go build -ldflags "-s -w -X github.com/majorcontext/keep/cmd/keep/cli.version=dev -X github.com/majorcontext/keep/cmd/keep/cli.commit=$$(git rev-parse --short HEAD) -X github.com/majorcontext/keep/cmd/keep/cli.date=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o keep ./cmd/keep

build-relay: ## Build the keep-mcp-relay binary
	go build -ldflags "-s -w" -o keep-mcp-relay ./cmd/keep-mcp-relay

build-gateway: ## Build the keep-llm-gateway binary
	go build -ldflags "-s -w" -o keep-llm-gateway ./cmd/keep-llm-gateway

test: test-unit ## Run all tests

test-unit: ## Run unit tests with race detector (use ARGS for filtering, e.g., ARGS='-run TestName')
	go test -race $(ARGS) ./...

test-integration: ## Run integration tests (builds CLI binary)
	go test -tags=integration -v ./cmd/keep/cli/

lint: lint-yaml ## Run all linters (requires golangci-lint v2)
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Install from https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run

lint-yaml: ## Check YAML formatting
	go run github.com/google/yamlfmt/cmd/yamlfmt@latest -lint .

fix: fix-go fix-yaml ## Auto-fix all linter and formatter issues

fix-go: ## Auto-fix Go formatting and lint issues (requires golangci-lint v2)
	gofmt -w .
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Install from https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run --fix

fix-yaml: ## Auto-fix YAML formatting
	go run github.com/google/yamlfmt/cmd/yamlfmt@latest .

coverage: ## Generate test coverage report
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

snapshot: ## Build a local release snapshot with GoReleaser
	@which goreleaser > /dev/null || (echo "goreleaser not installed. Install from https://goreleaser.com/install/" && exit 1)
	goreleaser release --snapshot --clean

clean: ## Clean build artifacts and coverage files
	rm -f keep keep-mcp-relay keep-llm-gateway
	rm -f coverage.out coverage.html
	rm -rf dist/
	go clean
