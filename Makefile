.PHONY: build test test-cover lint fmt vet run tidy clean release help

BINARY  := seed-hunter
PKG     := ./...
VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Cross-compilation targets for `make release`. Add or remove rows as
# needed; each entry expands into a binary named seed-hunter-<os>-<arch>.
RELEASE_PLATFORMS := \
	darwin/arm64 \
	darwin/amd64 \
	linux/amd64 \
	linux/arm64

LDFLAGS := -s -w \
	-X github.com/Chemaclass/seed-hunter/cmd.Version=$(VERSION) \
	-X github.com/Chemaclass/seed-hunter/cmd.Commit=$(COMMIT) \
	-X github.com/Chemaclass/seed-hunter/cmd.BuildDate=$(DATE)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the seed-hunter binary for the host platform
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

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

release: ## Cross-compile release binaries into dist/ (set VERSION=x.y.z)
	@if [ "$(VERSION)" = "dev" ]; then \
		echo "VERSION must be set, e.g. make release VERSION=0.1.0"; \
		exit 1; \
	fi
	@rm -rf dist && mkdir -p dist
	@for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		out=dist/$(BINARY)-$(VERSION)-$$os-$$arch; \
		echo "→ building $$out"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			go build -trimpath -ldflags "$(LDFLAGS)" -o $$out . || exit 1; \
	done
	@echo
	@echo "release artifacts:"
	@ls -la dist/

clean: ## Remove built artifacts
	rm -f $(BINARY) coverage.out
	rm -f *.db
	rm -rf dist/
