FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go.mod and download dependencies
COPY go.mod ./
RUN go mod download

# Copy test files
COPY *.go ./

# Build the test binary
RUN CGO_ENABLED=0 GOOS=linux go test -c -o /e2e-tests ./...

# Final stage - minimal image for running tests
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy the test binary
COPY --from=builder /e2e-tests /app/e2e-tests

# Set environment variables
ENV TESTAPI_URL=http://testapi:8080

# Run the tests
ENTRYPOINT ["/app/e2e-tests", "-test.v"]
