.PHONY: build test lint fmt run tidy clean help

BINARY := seed-hunter
PKG    := ./...

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the seed-hunter binary
	go build -o $(BINARY) .

test: ## Run all tests
	go test -race -count=1 $(PKG)

test-cover: ## Run tests with coverage
	go test -race -count=1 -coverprofile=coverage.out $(PKG)
	go tool cover -func=coverage.out

lint: ## Run golangci-lint
	golangci-lint run $(PKG)

fmt: ## Format Go source files
	gofmt -s -w .
	@command -v goimports >/dev/null 2>&1 && goimports -w . || true

vet: ## Run go vet
	go vet $(PKG)

run: build ## Build and run with --help
	./$(BINARY) --help

tidy: ## Tidy go.mod
	go mod tidy

clean: ## Remove built artifacts
	rm -f $(BINARY) coverage.out
	rm -f *.db
