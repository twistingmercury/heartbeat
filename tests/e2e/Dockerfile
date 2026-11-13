FROM ghcr.io/twistingmercury/golang-tooling:go1.27.3@sha256:12277dd36b15d77a0a938f5cca30724941c540a28bd95ff25357b3ead8bff4b4 AS builder

WORKDIR /app

# Copy go.mod and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy test files
COPY *.go ./

# Build the test binary
RUN CGO_ENABLED=0 GOOS=linux go test -c -o /e2e-tests ./...

# Final stage - minimal image for running tests
FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy the test binary
COPY --from=builder /e2e-tests /app/e2e-tests

# Set environment variables
ENV TESTAPI_URL=http://testapi:8080

# Run the tests
ENTRYPOINT ["/app/e2e-tests", "-test.v"]
