#!/usr/bin/env bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# shellcheck source=../scripts/print.sh
source "${PROJECT_DIR}/scripts/print.sh"

SKIP_E2E="${SKIP_E2E:-false}"

unit_test() {
	go mod tidy
	print::info "running unit tests..."
	if ! go test -v "${PROJECT_DIR}/..."; then
		print::error "unit tests failed"
		return 1
	fi
}

build() {
	print::info "building package..."
	if ! go build ./...; then
		print::error "build failed"
		return 1
	fi
}

e2e_test() {
	print::info "running end-to-end tests..."
	if ! "${PROJECT_DIR}/tests/e2e/test-runner.sh" run; then
		print::error "e2e tests failed"
		return 1
	fi
}

main() {
	unit_test && build || return 1

	if [ "${SKIP_E2E}" = "true" ]; then
		print::info "Skipping E2E tests (SKIP_E2E=true)"
	else
		e2e_test || return 1
	fi

	print::info "build completed successfully"
	return 0
}

main "$@"
