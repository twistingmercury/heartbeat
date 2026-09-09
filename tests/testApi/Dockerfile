FROM ghcr.io/twistingmercury/golang-tooling:go1.27.1 AS builder

RUN adduser -D -g '' -u 10001 appuser

RUN apk add --no-cache tzdata
ENV TZ=America/New_York
RUN cp /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

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

FROM alpine:latest AS runner

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
