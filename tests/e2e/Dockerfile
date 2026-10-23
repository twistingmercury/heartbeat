FROM ghcr.io/twistingmercury/golang-tooling:go1.27.3@sha256:c0d4ae73afa6c491cfc280802d3539d82937603f7abccc270a797c0ecfe1a159 AS builder

WORKDIR /app

# Copy go.mod and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy test files
COPY *.go ./

# Build the test binary
RUN CGO_ENABLED=0 GOOS=linux go test -c -o /e2e-tests ./...

# Final stage - minimal image for running tests
FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy the test binary
COPY --from=builder /e2e-tests /app/e2e-tests

# Set environment variables
ENV TESTAPI_URL=http://testapi:8080

# Run the tests
ENTRYPOINT ["/app/e2e-tests", "-test.v"]
