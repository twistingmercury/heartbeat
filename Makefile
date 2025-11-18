.PHONY: test help

default: help

help:
	@echo "\n-------------------------------------------------------------------------"
	@echo " Available targets"
	@echo "    make test - runs all tests and produces a coverage report"
	@echo "    make      - prints this help message"
	@echo "-------------------------------------------------------------------------\n"

test:
	go clean -testcache
	go test . -v -coverprofile=coverage.out
	go tool cover -html=coverage.out