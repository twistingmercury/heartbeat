FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

RUN adduser -D -g '' -u 10001 appuser

WORKDIR /build

COPY go.mod go.sum ./
COPY tests/testApi/go.mod tests/testApi/go.sum ./tests/testApi/

WORKDIR /build/tests/testApi

RUN go mod download

WORKDIR /build
COPY . .

WORKDIR /build/tests/testApi

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -trimpath \
    -o /build/testapi \
    .

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# Required for Go's time.LoadLocation() since scratch has no timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /build/testapi /testapi

USER appuser:appuser

EXPOSE 8080

ENV GIN_MODE=release

ENTRYPOINT ["/testapi"]
