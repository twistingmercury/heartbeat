#!/bin/bash
#
# E2E Test Runner for Heartbeat TestApi
#
# This script orchestrates the E2E test infrastructure and runs tests.
# It handles service startup, health checks, test execution, and cleanup.
#
# Usage:
#   ./test-runner.sh [command]
#
# Commands:
#   up       - Start infrastructure only (no tests)
#   down     - Stop and cleanup infrastructure
#   test     - Run tests against existing infrastructure
#   run      - Start infrastructure, run tests, then cleanup (default)
#   logs     - Show logs from all services
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
COMPOSE_FILE="docker-compose.yaml"
TESTAPI_URL="${TESTAPI_URL:-http://localhost:8080}"
MAX_WAIT_SECONDS=180
HEALTH_CHECK_INTERVAL=5

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Wait for a service to be healthy
wait_for_service() {
    local service_name=$1
    local url=$2
    local max_attempts=$((MAX_WAIT_SECONDS / HEALTH_CHECK_INTERVAL))
    local attempt=1

    log_info "Waiting for $service_name to be healthy..."

    while [ $attempt -le $max_attempts ]; do
        if curl -sf "$url" > /dev/null 2>&1; then
            log_info "$service_name is healthy"
            return 0
        fi
        log_info "Attempt $attempt/$max_attempts: $service_name not ready, waiting..."
        sleep $HEALTH_CHECK_INTERVAL
        attempt=$((attempt + 1))
    done

    log_error "$service_name failed to become healthy after $MAX_WAIT_SECONDS seconds"
    return 1
}

# Start infrastructure
start_infra() {
    log_info "Starting E2E test infrastructure..."
    docker compose -f "$COMPOSE_FILE" up -d cassandra rabbitmq

    log_info "Waiting for Cassandra to be ready (this may take a while)..."
    local max_attempts=40
    local attempt=1
    while [ $attempt -le $max_attempts ]; do
        if docker compose -f "$COMPOSE_FILE" exec -T cassandra cqlsh -e "SELECT release_version FROM system.local" > /dev/null 2>&1; then
            log_info "Cassandra is ready"
            break
        fi
        if [ $attempt -eq $max_attempts ]; then
            log_error "Cassandra failed to become ready"
            return 1
        fi
        log_info "Attempt $attempt/$max_attempts: Cassandra not ready, waiting..."
        sleep 5
        attempt=$((attempt + 1))
    done

    log_info "Waiting for RabbitMQ to be ready..."
    max_attempts=20
    attempt=1
    while [ $attempt -le $max_attempts ]; do
        if docker compose -f "$COMPOSE_FILE" exec -T rabbitmq rabbitmq-diagnostics -q ping > /dev/null 2>&1; then
            log_info "RabbitMQ is ready"
            break
        fi
        if [ $attempt -eq $max_attempts ]; then
            log_error "RabbitMQ failed to become ready"
            return 1
        fi
        log_info "Attempt $attempt/$max_attempts: RabbitMQ not ready, waiting..."
        sleep 3
        attempt=$((attempt + 1))
    done

    log_info "Starting testapi service..."
    docker compose -f "$COMPOSE_FILE" up -d testapi

    # Wait for testapi
    wait_for_service "testapi" "${TESTAPI_URL}/health"

    log_info "All services are ready"
}

# Stop infrastructure
stop_infra() {
    log_info "Stopping E2E test infrastructure..."
    docker compose -f "$COMPOSE_FILE" down -v --remove-orphans
    log_info "Infrastructure stopped and cleaned up"
}

# Run tests locally (against running infrastructure)
run_tests_local() {
    log_info "Running E2E tests locally..."
    cd "$SCRIPT_DIR"

    # Set environment variable for test
    export TESTAPI_URL="${TESTAPI_URL}"

    go test -v -race -count=1 ./...
    local exit_code=$?

    if [ $exit_code -eq 0 ]; then
        log_info "All E2E tests passed"
    else
        log_error "Some E2E tests failed"
    fi

    return $exit_code
}

# Run tests in Docker container
run_tests_docker() {
    log_info "Running E2E tests in Docker..."
    docker compose -f "$COMPOSE_FILE" up --build --abort-on-container-exit e2e-tests
    local exit_code=$?

    if [ $exit_code -eq 0 ]; then
        log_info "All E2E tests passed"
    else
        log_error "Some E2E tests failed"
    fi

    return $exit_code
}

# Show logs
show_logs() {
    docker compose -f "$COMPOSE_FILE" logs -f
}

# Main execution
main() {
    local command="${1:-run}"

    case "$command" in
        up)
            start_infra
            ;;
        down)
            stop_infra
            ;;
        test)
            run_tests_local
            ;;
        test-docker)
            run_tests_docker
            ;;
        run)
            start_infra
            local test_exit_code=0
            run_tests_local || test_exit_code=$?
            stop_infra
            exit $test_exit_code
            ;;
        run-docker)
            docker compose -f "$COMPOSE_FILE" up --build --abort-on-container-exit
            local exit_code=$?
            stop_infra
            exit $exit_code
            ;;
        logs)
            show_logs
            ;;
        *)
            echo "Usage: $0 {up|down|test|test-docker|run|run-docker|logs}"
            echo ""
            echo "Commands:"
            echo "  up          - Start infrastructure only"
            echo "  down        - Stop and cleanup infrastructure"
            echo "  test        - Run tests locally against running infrastructure"
            echo "  test-docker - Run tests in Docker against running infrastructure"
            echo "  run         - Start infra, run tests locally, cleanup (default)"
            echo "  run-docker  - Start infra, run tests in Docker, cleanup"
            echo "  logs        - Show logs from all services"
            exit 1
            ;;
    esac
}

main "$@"
