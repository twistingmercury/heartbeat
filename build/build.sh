#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${PROJECT_DIR}/scripts/print.sh"

IMAGE_NAME="heartbeat-build:latest"
IS_LOCAL="${IS_LOCAL:-0}"
COMPOSE_FILE="${PROJECT_DIR}/tests/docker-compose.yaml"

cleanup() {
    local exit_status=$?
    local cleanup_status=0

    docker compose -f "${COMPOSE_FILE}" down --volumes --remove-orphans || cleanup_status=$?

    if [[ "${IS_LOCAL}" == "0" ]]; then
        docker image rm "${IMAGE_NAME}" >/dev/null 2>&1 || true
        docker image rm tests-e2e_tests:latest >/dev/null 2>&1 || true
        docker image rm tests-testapi:latest >/dev/null 2>&1 || true
        docker network rm tests_e2e_network >/dev/null 2>&1 || true
    fi

    if [[ "${exit_status}" -eq 0 && "${cleanup_status}" -ne 0 ]]; then
        exit_status="${cleanup_status}"
    fi



    exit "${exit_status}"
}

trap cleanup EXIT

build(){
    docker build -t "${IMAGE_NAME}" -f "${SCRIPT_DIR}/Dockerfile" "${PROJECT_DIR}"
}

test(){
    docker compose -f "${COMPOSE_FILE}" up --build --abort-on-container-exit --exit-code-from e2e_tests
}

main(){
    build
    test
}

main "$@"
