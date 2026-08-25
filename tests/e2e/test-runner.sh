#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
COMPOSE_FILE="${SCRIPT_DIR}/../docker-compose.yaml"

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Stop infrastructure
stop_infra() {
    log_info "Stopping E2E test infrastructure..."
    docker compose -f "$COMPOSE_FILE" down -v --remove-orphans
    log_info "Infrastructure stopped and cleaned up"
}

# Run tests in Docker container
run_tests_docker() {
    log_info "Running E2E tests in Docker..."
    local exit_code=0

    if docker compose -f "$COMPOSE_FILE" up --build --abort-on-container-exit \
        --exit-code-from e2e_tests e2e_tests; then
        exit_code=0
    else
        exit_code=$?
    fi

    if [ $exit_code -eq 0 ]; then
        log_info "All E2E tests passed"
    else
        log_error "Some E2E tests failed"
    fi

    return "$exit_code"
}

# Show logs
show_logs() {
    docker compose -f "$COMPOSE_FILE" logs -f
}

# Main execution
main() {
    trap 'exit_code=$?; stop_infra; exit "$exit_code"' EXIT
    run_tests_docker
}

main "$@"
