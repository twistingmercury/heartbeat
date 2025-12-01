#!/usr/bin/env bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# shellcheck source=../scripts/print.sh
source "${PROJECT_DIR}/scripts/print.sh"

IMAGE_NAME="heartbeat-build:latest"

print::info "Building Docker image: ${IMAGE_NAME}"
docker build -t "${IMAGE_NAME}" -f "${SCRIPT_DIR}/Dockerfile" "${PROJECT_DIR}"

print::info "Running containerized build (unit tests + build, skipping E2E)..."
docker run --rm -e SKIP_E2E=true "${IMAGE_NAME}"

print::info "Containerized build completed successfully"
