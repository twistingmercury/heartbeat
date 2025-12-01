.PHONY: build build-docker test e2e-up e2e-down e2e-test e2e-run e2e-logs e2e-clean help

default: help

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\n\033[1mAvailable targets:\033[0m\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""

build: ## Run the full build process; unit tests, build, e2e tests
	./build/build.sh

build-docker: ## Run unit tests and build inside Docker container
	./build/build-docker.sh

test: ## Run unit tests with coverage report
	go clean -testcache
	go test . -v -coverprofile=coverage.out
	go tool cover -html=coverage.out

e2e-up: ## Start E2E test infrastructure
	@./tests/e2e/test-runner.sh up

e2e-down: ## Stop E2E test infrastructure
	@./tests/e2e/test-runner.sh down

e2e-test: ## Run E2E tests (requires e2e-up first)
	@./tests/e2e/test-runner.sh test

e2e-run: ## Full E2E cycle: start, test, cleanup
	@./tests/e2e/test-runner.sh run

e2e-logs: ## Show E2E service logs
	@./tests/e2e/test-runner.sh logs

e2e-clean: ## Force cleanup E2E Docker resources
	cd tests/e2e && docker compose down -v --remove-orphans 2>/dev/null || true
