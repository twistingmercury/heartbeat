FROM ghcr.io/twistingmercury/golang-tooling:go1.27.3@sha256:d9246e817f7288970bc956e6e41fabec69bb4be10eb287c9b14f4ca769fd5a05 AS builder

WORKDIR /app

# Copy go.mod and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy test files
COPY *.go ./

# Build the test binary
RUN CGO_ENABLED=0 GOOS=linux go test -c -o /e2e-tests ./...

# Final stage - minimal image for running tests
FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy the test binary
COPY --from=builder /e2e-tests /app/e2e-tests

# Set environment variables
ENV TESTAPI_URL=http://testapi:8080

# Run the tests
ENTRYPOINT ["/app/e2e-tests", "-test.v"]
