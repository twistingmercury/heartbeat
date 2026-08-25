.PHONY: build build-docker test e2e-up e2e-down e2e-test e2e-run e2e-logs e2e-clean help

default: help

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\n\033[1mAvailable targets:\033[0m\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""

build: ## Run the full build process; unit tests, build, e2e tests
	./build/build.sh

analyze: ## Run linters, formatters, security scanners, etc
	go vet ./...
	goimports -w .
	golangci-lint run
	govulncheck ./...
	gosec -quiet -exclude-dir=tests ./...


test: analyze ## Run unit tests with coverage report
	go clean -testcache
	go test . -v -coverprofile=coverage.out
	go tool cover -html=coverage.out


