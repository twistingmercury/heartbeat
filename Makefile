.PHONY: build analyze test help

default: help

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\n\033[1mAvailable targets:\033[0m\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""

build: ## Run the full build process; unit tests, build, e2e tests
	./build/build.sh

analyze: ## Run linters, format check, security scanners (does not modify files)
	go vet ./...
	@test -z "$$(goimports -l .)" || (echo "goimports: these files need formatting:"; goimports -l .; exit 1)
	golangci-lint run
	govulncheck ./...
	gosec -quiet -exclude-dir=tests ./...

test: analyze ## Run unit tests with the race detector and print coverage
	go clean -testcache
	CGO_ENABLED=1 go test . -race -v -coverprofile=coverage.out
	go tool cover -func=coverage.out
